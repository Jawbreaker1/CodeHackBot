package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func applyArchiveToolRuntimeEnv(baseEnv []string, task TaskSpec, command, workDir string) ([]string, []string, error) {
	if !taskLikelyLocalFileWorkflow(task) {
		return baseEnv, nil, nil
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if base != "john" {
		return baseEnv, nil, nil
	}
	home := strings.TrimSpace(workDir)
	if home == "" {
		return baseEnv, nil, nil
	}
	johnHome := filepath.Join(home, ".john")
	if err := os.MkdirAll(johnHome, 0o755); err != nil {
		return nil, nil, fmt.Errorf("prepare john runtime dir: %w", err)
	}
	env := append([]string{}, baseEnv...)
	env = withEnvValue(env, "HOME", home)
	env = withEnvValue(env, "JOHN", johnHome)
	return env, []string{fmt.Sprintf("archive runtime guardrail: isolated john HOME/JOHN to %s", johnHome)}, nil
}

func adaptArchiveWorkflowCommand(cfg WorkerRunConfig, task TaskSpec, command string, args []string, workDir string) (string, []string, []string, bool, error) {
	decision := decideHardSupportException(task, hardSupportArchiveWorkflow)
	if !decision.Allowed {
		return command, args, nil, false, nil
	}
	if !taskLikelyLocalFileWorkflow(task) {
		return command, args, nil, false, nil
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "zip2john", "zipinfo", "fcrackzip":
		nextArgs, notes, changed, err := ensureArchiveZipInputArg(cfg, task, base, args, workDir)
		if err != nil {
			return command, args, nil, false, err
		}
		if base == "fcrackzip" {
			verboseArgs, verboseNotes, verboseChanged := adaptArchiveFcrackzipArgs(cfg, task, nextArgs)
			if verboseChanged {
				nextArgs = verboseArgs
				notes = append(notes, verboseNotes...)
				changed = true
			}
		}
		if !changed {
			return command, args, nil, false, nil
		}
		return command, nextArgs, annotateHardSupportNotes(notes, decision), true, nil
	case "unzip", "zip":
		nextCommand, nextArgs, notes, changed, err := ensureArchiveUnzipInputs(cfg, task, command, args, workDir)
		if err != nil {
			return command, args, nil, false, err
		}
		if !changed {
			return command, args, nil, false, nil
		}
		return nextCommand, nextArgs, annotateHardSupportNotes(notes, decision), true, nil
	case "john":
		nextArgs, notes, changed, err := ensureArchiveJohnInputs(cfg, task, args, workDir)
		if err != nil {
			return command, args, nil, false, err
		}
		johnArgs, johnNotes, johnChanged, err := adaptArchiveJohnArgs(nextArgs, workDir)
		if err != nil {
			return command, args, nil, false, err
		}
		if johnChanged {
			nextArgs = johnArgs
			notes = append(notes, johnNotes...)
			changed = true
		}
		if !changed {
			return command, args, nil, false, nil
		}
		return command, nextArgs, annotateHardSupportNotes(notes, decision), true, nil
	case "bash", "sh", "zsh":
		nextArgs, notes, changed, err := adaptArchiveExtractionShellArgs(cfg, task, args)
		if err != nil {
			return command, args, nil, false, err
		}
		if !changed {
			return command, args, nil, false, nil
		}
		return command, nextArgs, annotateHardSupportNotes(notes, decision), true, nil
	default:
		return command, args, nil, false, nil
	}
}

func ensureArchiveZipInputArg(cfg WorkerRunConfig, task TaskSpec, command string, args []string, workDir string) ([]string, []string, bool, error) {
	if hasNonFlagArg(args) {
		return args, nil, false, nil
	}
	zipPath, source, ok, err := findArchiveZipInputPath(cfg, task, workDir)
	if err != nil {
		return args, nil, false, err
	}
	if !ok {
		return args, nil, false, nil
	}
	next := append([]string{}, args...)
	next = append(next, zipPath)
	note := fmt.Sprintf("archive command adaptation: injected %s input from %s (%s)", command, source, zipPath)
	return next, []string{note}, true, nil
}

func ensureArchiveJohnInputs(cfg WorkerRunConfig, task TaskSpec, args []string, workDir string) ([]string, []string, bool, error) {
	next := append([]string{}, args...)
	notes := []string{}
	changed := false

	if _, hasHash := findJohnHashArg(next); !hasHash {
		hashPath, source, ok, err := findArchiveHashInputPath(cfg, task, workDir)
		if err != nil {
			return args, nil, false, err
		}
		if ok {
			next = append(next, hashPath)
			changed = true
			notes = append(notes, fmt.Sprintf("archive command adaptation: injected john hash input from %s (%s)", source, hashPath))
		}
	}
	if !johnHasWordlist(next) {
		if wordlistPath, source, ok := findArchiveWordlistPath(cfg, task); ok {
			next = append([]string{"--wordlist=" + wordlistPath}, next...)
			changed = true
			notes = append(notes, fmt.Sprintf("archive command adaptation: injected john wordlist from %s (%s)", source, wordlistPath))
		}
	}
	return next, notes, changed, nil
}

func ensureArchiveUnzipInputs(cfg WorkerRunConfig, task TaskSpec, command string, args []string, workDir string) (string, []string, []string, bool, error) {
	if len(args) > 0 {
		return command, args, nil, false, nil
	}
	zipPath, zipSource, zipOK, err := findArchiveZipInputPath(cfg, task, workDir)
	if err != nil {
		return command, args, nil, false, err
	}
	if !zipOK {
		return command, args, nil, false, nil
	}
	passwordPath, passSource, passOK, err := findRecoveredPasswordArtifactPath(cfg, task)
	if err != nil {
		return command, args, nil, false, err
	}
	if passOK {
		script := fmt.Sprintf("set -euo pipefail; unzip -P \"$(cat %s)\" -p %s", shellQuotePath(passwordPath), shellQuotePath(zipPath))
		notes := []string{
			fmt.Sprintf("archive command adaptation: rewrote unzip to password-backed proof extraction from %s (%s)", zipSource, zipPath),
			fmt.Sprintf("archive command adaptation: using recovered password source %s (%s)", passSource, passwordPath),
		}
		return "bash", []string{"-lc", script}, notes, true, nil
	}
	note := fmt.Sprintf("archive command adaptation: injected unzip listing input from %s (%s)", zipSource, zipPath)
	return "unzip", []string{"-l", zipPath}, []string{note}, true, nil
}

func findArchiveZipInputPath(cfg WorkerRunConfig, task TaskSpec, workDir string) (string, string, bool, error) {
	type candidate struct {
		path   string
		source string
	}
	ordered := []candidate{}
	seen := map[string]struct{}{}
	addCandidate := func(path, source string) {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "" {
			return
		}
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		ordered = append(ordered, candidate{path: clean, source: source})
	}

	roots := archiveCandidateRoots(cfg, workDir)
	hints := archiveZipNameHints(task)
	for _, root := range roots {
		for _, name := range hints {
			addCandidate(filepath.Join(root, name), "local workspace")
		}
	}
	if len(task.DependsOn) > 0 {
		depCandidates, err := collectDependencyArtifactCandidates(cfg, task.DependsOn)
		if err != nil {
			return "", "", false, err
		}
		for _, dep := range depCandidates {
			addCandidate(dep, "dependency artifact")
		}
	}
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "*.zip"))
		for _, match := range matches {
			addCandidate(match, "local workspace")
		}
	}

	for _, candidate := range ordered {
		if !isLikelyZipArchiveFile(candidate.path) {
			continue
		}
		return candidate.path, candidate.source, true, nil
	}
	return "", "", false, nil
}

func findArchiveHashInputPath(cfg WorkerRunConfig, task TaskSpec, workDir string) (string, string, bool, error) {
	type candidate struct {
		path   string
		source string
		score  int
	}
	all := []candidate{}
	addCandidate := func(path, source string) {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "" {
			return
		}
		info, err := os.Stat(clean)
		if err != nil || info.IsDir() || info.Size() == 0 {
			return
		}
		score := 0
		if strings.HasSuffix(strings.ToLower(clean), ".hash") {
			score += 2
		}
		if fileContainsPKZIPHash(clean) {
			score += 6
		}
		if score == 0 {
			return
		}
		all = append(all, candidate{path: clean, source: source, score: score})
	}

	if len(task.DependsOn) > 0 {
		depCandidates, err := collectDependencyArtifactCandidates(cfg, task.DependsOn)
		if err != nil {
			return "", "", false, err
		}
		for _, dep := range depCandidates {
			addCandidate(dep, "dependency artifact")
		}
	}
	for _, pattern := range []string{"*.hash", "*.txt"} {
		matches, _ := filepath.Glob(filepath.Join(workDir, pattern))
		for _, match := range matches {
			addCandidate(match, "task workspace")
		}
	}
	if len(all) == 0 {
		return "", "", false, nil
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].path < all[j].path
	})
	best := all[0]
	return best.path, best.source, true, nil
}

func findArchiveWordlistPath(cfg WorkerRunConfig, task TaskSpec) (string, string, bool) {
	if len(task.DependsOn) > 0 {
		depCandidates, err := collectDependencyArtifactCandidates(cfg, task.DependsOn)
		if err == nil {
			for _, candidate := range depCandidates {
				if !looksLikeWordlistPath(candidate) {
					continue
				}
				info, statErr := os.Stat(candidate)
				if statErr == nil && !info.IsDir() && info.Size() > 0 {
					return candidate, "dependency artifact", true
				}
			}
		}
	}
	if candidate, source := resolveWordlistCandidate("/usr/share/wordlists/rockyou.txt"); strings.TrimSpace(candidate) != "" {
		return candidate, source, true
	}
	return "", "", false
}

func archiveCandidateRoots(cfg WorkerRunConfig, workDir string) []string {
	roots := append([]string{}, localWorkspaceRoots(cfg)...)
	if strings.TrimSpace(workDir) != "" {
		roots = append(roots, workDir)
	}
	return appendUnique(nil, roots...)
}

func archiveZipNameHints(task TaskSpec) []string {
	candidates := []string{}
	for _, expected := range task.ExpectedArtifacts {
		base := strings.TrimSpace(filepath.Base(expected))
		if strings.HasSuffix(strings.ToLower(base), ".zip") {
			candidates = append(candidates, base)
		}
	}
	text := strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
		strings.Join(task.ExpectedArtifacts, " "),
		task.Action.Command,
		strings.Join(task.Action.Args, " "),
	}, " ")
	for _, match := range archiveNamePattern.FindAllString(strings.ToLower(text), -1) {
		if strings.TrimSpace(match) != "" {
			candidates = append(candidates, match)
		}
	}
	return appendUnique(nil, candidates...)
}

func hasNonFlagArg(args []string) bool {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" || strings.HasPrefix(trimmed, "-") {
			continue
		}
		return true
	}
	return false
}

func johnHasWordlist(args []string) bool {
	for i := 0; i < len(args); i++ {
		trimmed := strings.TrimSpace(args[i])
		if strings.HasPrefix(trimmed, "--wordlist=") {
			return true
		}
		if trimmed == "--wordlist" && i+1 < len(args) && strings.TrimSpace(args[i+1]) != "" {
			return true
		}
	}
	return false
}

func fcrackzipHasStrategy(args []string) bool {
	for i := 0; i < len(args); i++ {
		trimmed := strings.TrimSpace(args[i])
		switch trimmed {
		case "-D", "--dictionary", "-b", "--bruteforce", "-c", "--charset", "-l", "--length":
			return true
		}
		if strings.HasPrefix(trimmed, "--dictionary=") || strings.HasPrefix(trimmed, "--charset=") || strings.HasPrefix(trimmed, "--length=") {
			return true
		}
	}
	return false
}

func isLikelyZipArchiveFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() < 4 {
		return false
	}
	if strings.ToLower(filepath.Ext(path)) != ".zip" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(f, header); err != nil {
		return false
	}
	signatures := [][]byte{
		[]byte("PK\x03\x04"),
		[]byte("PK\x05\x06"),
		[]byte("PK\x07\x08"),
	}
	for _, sig := range signatures {
		if bytes.Equal(header, sig) {
			return true
		}
	}
	return false
}

func fileContainsPKZIPHash(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	if info.Size() > dependencyArtifactReferenceMaxBytes {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "$pkzip$")
}

func adaptArchiveJohnArgs(args []string, workDir string) ([]string, []string, bool, error) {
	next := append([]string{}, args...)
	notes := []string{}
	changed := false

	hashPath, hasHash := findJohnHashArg(next)
	if rewritten, ok := rewriteJohnFormat(next, "zip", "pkzip"); ok {
		next = rewritten
		changed = true
		if hasHash && isPKZIPHashFile(hashPath) {
			notes = append(notes, fmt.Sprintf("archive command adaptation: rewrote john format zip->pkzip for %s", hashPath))
		} else {
			notes = append(notes, "archive command adaptation: rewrote john format zip->pkzip for local zip hash workflow")
		}
	}

	potPath := filepath.Join(workDir, "john.pot")
	if !johnHasOption(next, "--pot") {
		next = append([]string{"--pot=" + potPath}, next...)
		changed = true
		notes = append(notes, fmt.Sprintf("archive command adaptation: set john --pot to %s", potPath))
	}

	return next, notes, changed, nil
}

func adaptArchiveFcrackzipArgs(cfg WorkerRunConfig, task TaskSpec, args []string) ([]string, []string, bool) {
	next := append([]string{}, args...)
	notes := []string{}
	changed := false
	if !hasCommandFlag(next, "-v") {
		next = append([]string{"-v"}, next...)
		notes = append(notes, "archive command adaptation: enabled fcrackzip verbose mode (-v) for evidence capture")
		changed = true
	}
	if !fcrackzipHasStrategy(next) {
		if wordlistPath, source, ok := findArchiveWordlistPath(cfg, task); ok {
			next = append([]string{"-D", "-u", "-p", wordlistPath}, next...)
			notes = append(notes, fmt.Sprintf("archive command adaptation: injected bounded fcrackzip dictionary strategy from %s (%s)", source, wordlistPath))
			changed = true
		}
	}
	return next, notes, changed
}

func adaptArchiveExtractionShellArgs(cfg WorkerRunConfig, task TaskSpec, args []string) ([]string, []string, bool, error) {
	if len(args) < 2 {
		return args, nil, false, nil
	}
	mode := strings.TrimSpace(args[0])
	if mode != "-c" && mode != "-lc" {
		return args, nil, false, nil
	}
	body := args[1]
	if strings.TrimSpace(body) == "" || !strings.Contains(strings.ToLower(body), "unzip -p") {
		return args, nil, false, nil
	}
	if !strings.Contains(strings.ToLower(body), "john_output") && !strings.Contains(strings.ToLower(body), "password_found") {
		return args, nil, false, nil
	}
	passwordPath, source, ok, err := findRecoveredPasswordArtifactPath(cfg, task)
	if err != nil {
		return args, nil, false, err
	}
	if !ok {
		return args, nil, false, nil
	}
	replacement := "unzip -P \"$(cat " + shellQuotePath(passwordPath) + ")\""
	rewritten := unzipPasswordPipelinePattern.ReplaceAllString(body, replacement)
	if rewritten == body {
		rewritten = strings.ReplaceAll(body, "$(cat password_found)", "\"$(cat "+shellQuotePath(passwordPath)+")\"")
		rewritten = strings.ReplaceAll(rewritten, "$(cat ./password_found)", "\"$(cat "+shellQuotePath(passwordPath)+")\"")
	}
	if rewritten == body {
		return args, nil, false, nil
	}
	next := append([]string{}, args...)
	next[1] = rewritten
	note := fmt.Sprintf("archive command adaptation: rewrote unzip password source to %s (%s)", source, passwordPath)
	return next, []string{note}, true, nil
}

func findRecoveredPasswordArtifactPath(cfg WorkerRunConfig, task TaskSpec) (string, string, bool, error) {
	if len(task.DependsOn) > 0 {
		depCandidates, err := collectDependencyArtifactCandidates(cfg, task.DependsOn)
		if err != nil {
			return "", "", false, err
		}
		if candidate := firstRecoveredPasswordCandidate(depCandidates); candidate != "" {
			return candidate, "dependency artifact", true, nil
		}
	}
	runArtifactDir := BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir
	entries := []string{}
	_ = filepath.WalkDir(runArtifactDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		entries = append(entries, path)
		return nil
	})
	if candidate := firstRecoveredPasswordCandidate(entries); candidate != "" {
		return candidate, "run artifact", true, nil
	}
	return "", "", false, nil
}

func firstRecoveredPasswordCandidate(paths []string) string {
	best := ""
	bestScore := -1
	for _, path := range paths {
		base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
		score := 0
		switch base {
		case "recovered_password.txt":
			score = 3
		case "password_found", "password_found.txt":
			score = 2
		}
		if score == 0 {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			best = path
		}
	}
	return best
}

func writeArchiveWorkflowSupplementalArtifacts(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, command string, args []string, workDir string) ([]string, error) {
	if !taskLikelyLocalFileWorkflow(task) {
		return nil, nil
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "john":
		return writeJohnSupplementalArtifacts(ctx, cfg, task, command, args, workDir)
	case "fcrackzip":
		return writeFcrackzipSupplementalArtifacts(cfg)
	default:
		return nil, nil
	}
}

func writeJohnSupplementalArtifacts(ctx context.Context, cfg WorkerRunConfig, task TaskSpec, command string, args []string, workDir string) ([]string, error) {
	hashPath, hasHash := findJohnHashArg(args)
	if !hasHash {
		return nil, nil
	}
	if info, err := os.Stat(hashPath); err != nil || info.IsDir() {
		return nil, nil
	}
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact dir: %w", err)
	}
	env, _, err := applyArchiveToolRuntimeEnv(os.Environ(), task, command, workDir)
	if err != nil {
		return nil, err
	}
	showCmd := exec.Command("john", "--show", "--format=pkzip", hashPath)
	showCmd.Dir = workDir
	showCmd.Env = env
	showOutput, showErr := runWorkerCommand(ctx, showCmd, workerCommandStopGrace)
	if showErr != nil && len(showOutput) == 0 {
		showOutput = []byte(showErr.Error() + "\n")
	}
	showPath := filepath.Join(artifactDir, "john_show.txt")
	if err := os.WriteFile(showPath, showOutput, 0o644); err != nil {
		return nil, fmt.Errorf("write john_show artifact: %w", err)
	}
	artifacts := []string{showPath}
	if recovered := parseJohnShowRecoveredPassword(showOutput); strings.TrimSpace(recovered) != "" {
		recoveredPath := filepath.Join(artifactDir, "recovered_password.txt")
		if err := os.WriteFile(recoveredPath, []byte(recovered+"\n"), 0o644); err != nil {
			return nil, fmt.Errorf("write recovered_password artifact: %w", err)
		}
		artifacts = append(artifacts, recoveredPath)
	}
	return artifacts, nil
}

func writeFcrackzipSupplementalArtifacts(cfg WorkerRunConfig) ([]string, error) {
	artifactDir := filepath.Join(BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir, cfg.TaskID)
	outputPath := filepath.Join(artifactDir, "fcrackzip_output.txt")
	output, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, nil
	}
	recovered := parseFcrackzipRecoveredPassword(output)
	if strings.TrimSpace(recovered) == "" {
		return nil, nil
	}
	recoveredPath := filepath.Join(artifactDir, "recovered_password.txt")
	if err := os.WriteFile(recoveredPath, []byte(recovered+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write recovered_password artifact: %w", err)
	}
	return []string{recoveredPath}, nil
}

func parseJohnShowRecoveredPassword(output []byte) string {
	lines := strings.Split(string(output), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "password hash") || strings.Contains(lower, "no password hashes") || strings.Contains(lower, "loaded ") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		password := strings.TrimSpace(parts[1])
		if password == "" {
			continue
		}
		return password
	}
	return ""
}

func parseFcrackzipRecoveredPassword(output []byte) string {
	re := regexp.MustCompile(`(?i)PASSWORD FOUND!!!!:\s+pw\s*==\s*(\S+)`)
	matches := re.FindSubmatch(output)
	if len(matches) == 2 {
		return strings.TrimSpace(string(matches[1]))
	}
	return ""
}

func findJohnHashArg(args []string) (string, bool) {
	for i := len(args) - 1; i >= 0; i-- {
		arg := strings.TrimSpace(args[i])
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.HasSuffix(strings.ToLower(arg), ".hash") {
			return arg, true
		}
	}
	return "", false
}

func isPKZIPHashFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "$pkzip$")
}

func rewriteJohnFormat(args []string, from, to string) ([]string, bool) {
	next := append([]string{}, args...)
	from = strings.ToLower(strings.TrimSpace(from))
	to = strings.ToLower(strings.TrimSpace(to))
	for i := 0; i < len(next); i++ {
		arg := strings.TrimSpace(next[i])
		lower := strings.ToLower(arg)
		if lower == "--format" && i+1 < len(next) {
			if strings.EqualFold(strings.TrimSpace(next[i+1]), from) {
				next[i+1] = to
				return next, true
			}
			return next, false
		}
		if strings.HasPrefix(lower, "--format=") {
			value := strings.TrimSpace(strings.TrimPrefix(lower, "--format="))
			if value == from {
				next[i] = "--format=" + to
				return next, true
			}
			return next, false
		}
	}
	return next, false
}

func johnHasOption(args []string, option string) bool {
	option = strings.ToLower(strings.TrimSpace(option))
	for _, raw := range args {
		arg := strings.ToLower(strings.TrimSpace(raw))
		if arg == option || strings.HasPrefix(arg, option+"=") {
			return true
		}
	}
	return false
}

func hasCommandFlag(args []string, flag string) bool {
	flag = strings.TrimSpace(flag)
	for _, arg := range args {
		if strings.TrimSpace(arg) == flag {
			return true
		}
	}
	return false
}
