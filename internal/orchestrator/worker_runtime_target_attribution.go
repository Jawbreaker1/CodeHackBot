package orchestrator

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type targetAttribution struct {
	Target     string   `json:"target"`
	Confidence string   `json:"confidence"`
	Source     string   `json:"source"`
	Evidence   []string `json:"evidence,omitempty"`
}

type persistedTargetAttribution struct {
	targetAttribution
	RunID     string    `json:"run_id,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

var nmapReportIPPattern = regexp.MustCompile(`(?i)Nmap scan report for (?:[^\n(]*\()?((?:\d{1,3}\.){3}\d{1,3})\)?`)

func taskNeedsTargetAttribution(task TaskSpec) bool {
	return taskRequiresVulnerabilityEvidence(task) || taskRequiresReportSynthesis(task)
}

func resolveTaskTargetAttribution(cfg WorkerRunConfig, task TaskSpec, scopePolicy *ScopePolicy) (targetAttribution, error) {
	if target := firstPinnedTaskTarget(task.Targets); target != "" {
		return targetAttribution{
			Target:     target,
			Confidence: "high",
			Source:     "task_target",
		}, nil
	}
	fromDeps, err := resolveTaskTargetAttributionFromDependencies(cfg, task.DependsOn, scopePolicy)
	if err != nil {
		return targetAttribution{}, err
	}
	if strings.TrimSpace(fromDeps.Target) != "" {
		return fromDeps, nil
	}
	return targetAttribution{}, fmt.Errorf("no resolved target attribution evidence for task %s", task.TaskID)
}

func resolveTaskTargetAttributionFromDependencies(cfg WorkerRunConfig, dependencies []string, scopePolicy *ScopePolicy) (targetAttribution, error) {
	if len(dependencies) == 0 {
		return targetAttribution{}, nil
	}
	base := BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir
	candidates := make([]targetAttribution, 0, len(dependencies))
	for _, dep := range compactStringSlice(dependencies) {
		depDir := filepath.Join(base, dep)
		info, err := os.Stat(depDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return targetAttribution{}, err
		}
		if !info.IsDir() {
			continue
		}
		found := false
		if err := filepath.WalkDir(depDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if strings.TrimSpace(strings.ToLower(filepath.Base(path))) != "resolved_target.json" {
				return nil
			}
			attr, parseErr := readPersistedTargetAttribution(path)
			if parseErr != nil || strings.TrimSpace(attr.Target) == "" {
				return nil
			}
			if scopePolicy != nil {
				if validateErr := scopePolicy.ValidateTaskTargets(TaskSpec{Targets: []string{attr.Target}}); validateErr != nil {
					return nil
				}
			}
			attr.Evidence = appendUnique(attr.Evidence, path)
			candidates = append(candidates, attr)
			found = true
			return nil
		}); err != nil {
			return targetAttribution{}, err
		}
		if found {
			continue
		}
		fromLogs, ok, parseErr := inferSingleNmapTargetFromDirectory(depDir, scopePolicy)
		if parseErr != nil {
			return targetAttribution{}, parseErr
		}
		if ok {
			candidates = append(candidates, fromLogs)
		}
	}
	if len(candidates) == 0 {
		return targetAttribution{}, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return targetConfidenceScore(candidates[i].Confidence) > targetConfidenceScore(candidates[j].Confidence)
	})
	best := candidates[0]
	best.Evidence = appendUnique(nil, best.Evidence...)
	return best, nil
}

func readPersistedTargetAttribution(path string) (targetAttribution, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return targetAttribution{}, err
	}
	var stored persistedTargetAttribution
	if err := json.Unmarshal(data, &stored); err != nil {
		return targetAttribution{}, err
	}
	attr := targetAttribution{
		Target:     strings.TrimSpace(stored.Target),
		Confidence: strings.TrimSpace(strings.ToLower(stored.Confidence)),
		Source:     strings.TrimSpace(stored.Source),
		Evidence:   appendUnique(nil, stored.Evidence...),
	}
	if attr.Confidence == "" {
		attr.Confidence = "medium"
	}
	return attr, nil
}

func inferSingleNmapTargetFromDirectory(dir string, scopePolicy *ScopePolicy) (targetAttribution, bool, error) {
	ips := map[string]struct{}{}
	evidence := []string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		text, readErr := readSmallText(path, 256*1024)
		if readErr != nil || strings.TrimSpace(text) == "" {
			return nil
		}
		matches := nmapReportIPPattern.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			return nil
		}
		fileMatched := false
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			ip := strings.TrimSpace(match[1])
			if net.ParseIP(ip) == nil {
				continue
			}
			if scopePolicy != nil {
				if validateErr := scopePolicy.ValidateTaskTargets(TaskSpec{Targets: []string{ip}}); validateErr != nil {
					continue
				}
			}
			ips[ip] = struct{}{}
			fileMatched = true
		}
		if fileMatched {
			evidence = append(evidence, path)
		}
		return nil
	})
	if err != nil {
		return targetAttribution{}, false, err
	}
	if len(ips) != 1 {
		return targetAttribution{}, false, nil
	}
	for ip := range ips {
		return targetAttribution{
			Target:     ip,
			Confidence: "medium",
			Source:     "dependency_nmap_single_host",
			Evidence:   appendUnique(nil, evidence...),
		}, true, nil
	}
	return targetAttribution{}, false, nil
}

func inferTaskCompletionTargetAttribution(task TaskSpec, output []byte) targetAttribution {
	if target := firstPinnedTaskTarget(task.Targets); target != "" {
		return targetAttribution{
			Target:     target,
			Confidence: "high",
			Source:     "task_target",
		}
	}
	matches := nmapReportIPPattern.FindAllStringSubmatch(string(output), -1)
	ips := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		ip := strings.TrimSpace(match[1])
		if net.ParseIP(ip) == nil {
			continue
		}
		ips[ip] = struct{}{}
	}
	if len(ips) == 1 {
		for ip := range ips {
			return targetAttribution{
				Target:     ip,
				Confidence: "medium",
				Source:     "single_host_observed",
			}
		}
	}
	return targetAttribution{}
}

func writeTargetAttributionArtifact(cfg WorkerRunConfig, task TaskSpec, attribution targetAttribution) (string, error) {
	target := strings.TrimSpace(attribution.Target)
	if target == "" {
		return "", nil
	}
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	payload := persistedTargetAttribution{
		targetAttribution: targetAttribution{
			Target:     target,
			Confidence: strings.TrimSpace(strings.ToLower(attribution.Confidence)),
			Source:     strings.TrimSpace(attribution.Source),
			Evidence:   appendUnique(nil, attribution.Evidence...),
		},
		RunID:     cfg.RunID,
		TaskID:    task.TaskID,
		CreatedAt: time.Now().UTC(),
	}
	if payload.Confidence == "" {
		payload.Confidence = "medium"
	}
	path := filepath.Join(artifactDir, "resolved_target.json")
	if err := WriteJSONAtomic(path, payload); err != nil {
		return "", err
	}
	return path, nil
}

func enforceAttributedCommandTarget(command string, args []string, target string) ([]string, string, bool) {
	target = strings.TrimSpace(target)
	if target == "" || !isNmapCommand(command) {
		return args, "", false
	}
	next := rewriteNmapTargetArgs(args, target)
	if stringSlicesEqual(next, args) {
		return args, "", false
	}
	note := fmt.Sprintf("enforced resolved target attribution: rewrote nmap execution target(s) to %s", target)
	return next, note, true
}

func rewriteNmapTargetArgs(args []string, target string) []string {
	out := make([]string, 0, len(args)+1)
	skipValue := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if skipValue {
			out = append(out, arg)
			skipValue = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			out = append(out, arg)
			if nmapOptionRequiresValue(arg) && !strings.Contains(arg, "=") {
				skipValue = true
			}
			continue
		}
		if looksLikeScanTarget(arg) {
			continue
		}
		out = append(out, arg)
	}
	out = append(out, target)
	return out
}

func nmapOptionRequiresValue(option string) bool {
	switch strings.ToLower(strings.TrimSpace(option)) {
	case "-p", "-pa", "-ps", "-pu", "-oN", "-oX", "-oG", "-oA", "-iL", "-s", "-e", "-T",
		"--script", "--script-timeout", "--host-timeout", "--max-retries", "--max-rate",
		"--top-ports", "--dns-servers", "--min-rate", "--min-hostgroup", "--max-hostgroup",
		"--source-port", "--datadir", "--proxies":
		return true
	default:
		return false
	}
}

func looksLikeScanTarget(value string) bool {
	token := strings.TrimSpace(value)
	if token == "" {
		return false
	}
	if _, _, err := net.ParseCIDR(token); err == nil {
		return true
	}
	if net.ParseIP(token) != nil {
		return true
	}
	if strings.Contains(token, "/") {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func firstPinnedTaskTarget(targets []string) string {
	for _, raw := range targets {
		target := strings.TrimSpace(raw)
		if target == "" {
			continue
		}
		if isCIDRTarget(target) {
			continue
		}
		return target
	}
	return ""
}

func isCIDRTarget(target string) bool {
	_, _, err := net.ParseCIDR(strings.TrimSpace(target))
	return err == nil
}

func targetConfidenceScore(confidence string) int {
	switch strings.TrimSpace(strings.ToLower(confidence)) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func readSmallText(path string, maxBytes int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[:maxBytes]
	}
	return string(data), nil
}
