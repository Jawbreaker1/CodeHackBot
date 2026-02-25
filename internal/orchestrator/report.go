package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	reportFindingVerified   = "VERIFIED"
	reportFindingUnverified = "UNVERIFIED"
	reportMaxEvidenceItems  = 8
	reportMaxLinkedItems    = 10
	reportMaxProgressItems  = 3
	reportMaxTaskArtifacts  = 6
	reportMaxTaskFindings   = 3
	reportCommandPreviewMax = 180
)

type artifactRecord struct {
	Artifact   Artifact
	RecordPath string
}

type reportFinding struct {
	Finding             Finding
	Verification        string
	LinkedEvidence      []string
	UnverifiedRationale string
}

type reportTaskNarrative struct {
	TaskID            string
	Title             string
	Goal              string
	DependsOn         []string
	RiskLevel         string
	Strategy          string
	ExpectedArtifacts []string

	Started   bool
	Completed bool
	Failed    bool

	Attempt      int
	WorkerID     string
	StartedAt    time.Time
	TransitionAt time.Time

	CompletionReason string
	FailureReason    string
	FailureError     string

	LastCommand       string
	ProgressMessages  []string
	ProducedArtifacts []string
	ArtifactPaths     []string
	FindingSummaries  []string
}

func (m *Manager) AssembleRunReport(runID, outPath string) (string, error) {
	if strings.TrimSpace(runID) == "" {
		return "", fmt.Errorf("run id is required")
	}
	paths := BuildRunPaths(m.SessionsDir, runID)
	if strings.TrimSpace(outPath) == "" {
		outPath = filepath.Join(paths.Root, "report.md")
	}

	plan, err := m.LoadRunPlan(runID)
	if err != nil {
		return "", err
	}
	status, err := m.Status(runID)
	if err != nil {
		return "", err
	}
	findings, err := m.ListFindings(runID)
	if err != nil {
		return "", err
	}
	artifacts, err := m.listArtifactRecords(runID)
	if err != nil {
		return "", err
	}
	events, err := m.Events(runID, 0)
	if err != nil {
		return "", err
	}

	sort.Slice(findings, func(i, j int) bool {
		si := severityRank(findings[i].Severity)
		sj := severityRank(findings[j].Severity)
		if si != sj {
			return si > sj
		}
		ci := confidenceRank(findings[i].Confidence)
		cj := confidenceRank(findings[j].Confidence)
		if ci != cj {
			return ci > cj
		}
		return findings[i].Title < findings[j].Title
	})
	reportFindings := buildReportFindings(findings, artifacts)
	taskNarratives := buildTaskNarratives(plan, events, artifacts, reportFindings)
	verifiedCount := 0
	for _, item := range reportFindings {
		if item.Verification == reportFindingVerified {
			verifiedCount++
		}
	}
	unverifiedCount := len(reportFindings) - verifiedCount
	completedTasks, failedTasks, startedTasks := summarizeNarrativeTaskStates(taskNarratives)
	runStartedAt, runEndedAt, hasRunWindow := runWindow(events)

	var b strings.Builder
	fmt.Fprintf(&b, "# Orchestrator Run Report\n\n")
	fmt.Fprintf(&b, "- Run ID: `%s`\n", runID)
	fmt.Fprintf(&b, "- Generated: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- State: `%s`\n", status.State)
	fmt.Fprintf(&b, "- Active workers: %d\n", status.ActiveWorkers)
	fmt.Fprintf(&b, "- Queued tasks: %d\n", status.QueuedTasks)
	fmt.Fprintf(&b, "- Running tasks: %d\n", status.RunningTasks)
	fmt.Fprintf(&b, "- Findings: %d\n", len(reportFindings))
	fmt.Fprintf(&b, "- Verified findings: %d\n", verifiedCount)
	fmt.Fprintf(&b, "- Unverified findings: %d\n", unverifiedCount)
	fmt.Fprintf(&b, "- Artifacts: %d\n", len(artifacts))
	if hasRunWindow {
		fmt.Fprintf(&b, "- Runtime window: %s\n", formatRuntimeWindow(runStartedAt, runEndedAt))
	}

	fmt.Fprintf(&b, "\n## Scope\n\n")
	if len(plan.Scope.Networks) == 0 && len(plan.Scope.Targets) == 0 && len(plan.Scope.DenyTargets) == 0 {
		fmt.Fprintf(&b, "- Not specified\n")
	} else {
		for _, network := range plan.Scope.Networks {
			fmt.Fprintf(&b, "- Network: `%s`\n", network)
		}
		for _, target := range plan.Scope.Targets {
			fmt.Fprintf(&b, "- Target: `%s`\n", target)
		}
		for _, denied := range plan.Scope.DenyTargets {
			fmt.Fprintf(&b, "- Deny: `%s`\n", denied)
		}
	}

	fmt.Fprintf(&b, "\n## Plan Overview\n\n")
	if goal := strings.TrimSpace(plan.Metadata.Goal); goal != "" {
		fmt.Fprintf(&b, "- Objective: %s\n", goal)
	}
	fmt.Fprintf(&b, "- Planned tasks: %d\n", len(plan.Tasks))
	fmt.Fprintf(&b, "- Max parallelism: %d\n", plan.MaxParallelism)
	if len(plan.Constraints) > 0 {
		fmt.Fprintf(&b, "- Constraints:\n")
		for _, constraint := range plan.Constraints {
			fmt.Fprintf(&b, "  - `%s`\n", constraint)
		}
	}
	if len(plan.Tasks) > 0 {
		fmt.Fprintf(&b, "- Task graph:\n")
		for _, task := range plan.Tasks {
			title := reportTaskTitle(task.TaskID, task.Title, task.Goal)
			fmt.Fprintf(&b, "  - `%s` %s\n", task.TaskID, title)
			if len(task.DependsOn) == 0 {
				fmt.Fprintf(&b, "    - Depends on: none\n")
			} else {
				fmt.Fprintf(&b, "    - Depends on: `%s`\n", strings.Join(task.DependsOn, "`, `"))
			}
			if strings.TrimSpace(task.RiskLevel) != "" {
				fmt.Fprintf(&b, "    - Risk level: `%s`\n", task.RiskLevel)
			}
		}
	}

	fmt.Fprintf(&b, "\n## Execution Narrative\n\n")
	fmt.Fprintf(&b, "- Tasks started: %d/%d\n", startedTasks, len(taskNarratives))
	fmt.Fprintf(&b, "- Tasks completed: %d/%d\n", completedTasks, len(taskNarratives))
	fmt.Fprintf(&b, "- Tasks failed: %d/%d\n", failedTasks, len(taskNarratives))
	for _, taskView := range taskNarratives {
		fmt.Fprintf(&b, "\n### `%s` %s\n\n", taskView.TaskID, reportTaskTitle(taskView.TaskID, taskView.Title, taskView.Goal))
		fmt.Fprintf(&b, "- Intent: %s\n", nonEmptyOrFallback(strings.TrimSpace(taskView.Goal), "not specified"))
		if len(taskView.DependsOn) == 0 {
			fmt.Fprintf(&b, "- Dependencies: none\n")
		} else {
			fmt.Fprintf(&b, "- Dependencies: `%s`\n", strings.Join(taskView.DependsOn, "`, `"))
		}
		if strings.TrimSpace(taskView.LastCommand) != "" {
			fmt.Fprintf(&b, "- Execution method: `%s`\n", taskView.LastCommand)
		}
		fmt.Fprintf(&b, "- Outcome: `%s`\n", narrativeTaskOutcome(taskView))
		if taskView.Attempt > 0 {
			fmt.Fprintf(&b, "- Attempt: %d\n", taskView.Attempt)
		}
		if worker := strings.TrimSpace(taskView.WorkerID); worker != "" {
			fmt.Fprintf(&b, "- Worker: `%s`\n", worker)
		}
		if !taskView.StartedAt.IsZero() && !taskView.TransitionAt.IsZero() {
			fmt.Fprintf(&b, "- Duration: %s\n", formatDuration(taskView.TransitionAt.Sub(taskView.StartedAt)))
		}
		if len(taskView.ProgressMessages) > 0 {
			fmt.Fprintf(&b, "- Key execution notes:\n")
			for _, message := range taskView.ProgressMessages {
				fmt.Fprintf(&b, "  - %s\n", message)
			}
		}
		taskArtifacts := reportTaskArtifacts(taskView)
		if len(taskArtifacts) > 0 {
			fmt.Fprintf(&b, "- Key outputs:\n")
			for _, artifact := range taskArtifacts {
				fmt.Fprintf(&b, "  - `%s`\n", renderReportPath(artifact, paths.Root))
			}
		}
		if len(taskView.FindingSummaries) > 0 {
			fmt.Fprintf(&b, "- Related findings:\n")
			for _, summary := range taskView.FindingSummaries {
				fmt.Fprintf(&b, "  - %s\n", summary)
			}
		}
	}

	fmt.Fprintf(&b, "\n## Findings\n\n")
	if len(reportFindings) == 0 {
		fmt.Fprintf(&b, "- No findings were merged for this run.\n")
	} else {
		for _, findingView := range reportFindings {
			finding := findingView.Finding
			fmt.Fprintf(&b, "### [%s] %s (%s / %s)\n\n", findingView.Verification, finding.Title, strings.ToUpper(finding.Severity), strings.ToUpper(finding.Confidence))
			fmt.Fprintf(&b, "- Verification: `%s`\n", findingView.Verification)
			if findingView.Verification == reportFindingUnverified && strings.TrimSpace(findingView.UnverifiedRationale) != "" {
				fmt.Fprintf(&b, "- Claim status: `%s` (%s)\n", reportFindingUnverified, findingView.UnverifiedRationale)
			}
			if finding.Target != "" {
				fmt.Fprintf(&b, "- Target: `%s`\n", finding.Target)
			}
			if finding.FindingType != "" {
				fmt.Fprintf(&b, "- Type: `%s`\n", finding.FindingType)
			}
			if location := finding.Metadata["location"]; location != "" {
				fmt.Fprintf(&b, "- Location: `%s`\n", location)
			}
			if len(finding.Evidence) > 0 {
				fmt.Fprintf(&b, "- Evidence:\n")
				for _, evidence := range truncateWithOverflow(finding.Evidence, reportMaxEvidenceItems) {
					fmt.Fprintf(&b, "  - %s\n", renderReportPath(evidence, paths.Root))
				}
				if omitted := len(finding.Evidence) - reportMaxEvidenceItems; omitted > 0 {
					fmt.Fprintf(&b, "  - ... %d more evidence entries (see artifacts/events for full detail)\n", omitted)
				}
			} else if findingView.Verification == reportFindingUnverified {
				fmt.Fprintf(&b, "- Evidence:\n")
				fmt.Fprintf(&b, "  - %s: no direct evidence statements were recorded for this finding.\n", reportFindingUnverified)
			}
			if len(findingView.LinkedEvidence) > 0 {
				fmt.Fprintf(&b, "- Linked artifact/log evidence:\n")
				for _, link := range truncateWithOverflow(findingView.LinkedEvidence, reportMaxLinkedItems) {
					fmt.Fprintf(&b, "  - %s\n", renderReportPath(link, paths.Root))
				}
				if omitted := len(findingView.LinkedEvidence) - reportMaxLinkedItems; omitted > 0 {
					fmt.Fprintf(&b, "  - ... %d more linked entries (see Artifacts section)\n", omitted)
				}
			} else {
				fmt.Fprintf(&b, "- Linked artifact/log evidence: `%s`\n", reportFindingUnverified)
			}
			if len(finding.Sources) > 0 {
				fmt.Fprintf(&b, "- Sources:\n")
				for _, source := range finding.Sources {
					name := source.Source
					if strings.TrimSpace(name) == "" {
						name = source.WorkerID
					}
					fmt.Fprintf(&b, "  - %s (event `%s`, task `%s`)\n", name, source.EventID, source.TaskID)
				}
			}
			if len(finding.Conflicts) > 0 {
				fmt.Fprintf(&b, "- Conflicts retained:\n")
				for _, conflict := range finding.Conflicts {
					fmt.Fprintf(&b, "  - `%s`: `%s` vs `%s` -> `%s` (event `%s`)\n", conflict.Field, conflict.ExistingValue, conflict.IncomingValue, conflict.Resolution, conflict.IncomingEvent)
				}
			}
			fmt.Fprintf(&b, "\n")
		}
	}

	fmt.Fprintf(&b, "## Artifacts\n\n")
	if len(artifacts) == 0 {
		fmt.Fprintf(&b, "- No artifacts were merged for this run.\n")
	} else {
		for _, item := range artifacts {
			title := strings.TrimSpace(item.Artifact.Title)
			if title == "" {
				title = filepath.Base(item.RecordPath)
			}
			fmt.Fprintf(&b, "- `%s` (%s)\n", title, item.Artifact.Type)
			if item.Artifact.Path != "" {
				fmt.Fprintf(&b, "  - Path: `%s`\n", renderReportPath(item.Artifact.Path, paths.Root))
			}
			fmt.Fprintf(&b, "  - Record: `%s`\n", renderReportPath(item.RecordPath, paths.Root))
		}
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return outPath, nil
}

func buildReportFindings(findings []Finding, artifacts []artifactRecord) []reportFinding {
	linksByTask := artifactLinksByTask(artifacts)
	out := make([]reportFinding, 0, len(findings))
	for _, finding := range findings {
		links := make([]string, 0, 4)
		links = append(links, linksByTask[strings.TrimSpace(finding.TaskID)]...)
		for _, source := range finding.Sources {
			links = append(links, linksByTask[strings.TrimSpace(source.TaskID)]...)
		}
		for _, evidence := range finding.Evidence {
			link := evidencePathLike(evidence)
			if link != "" {
				links = append(links, link)
			}
		}
		links = dedupeTrimmed(links)
		view := reportFinding{
			Finding:        finding,
			Verification:   reportFindingVerified,
			LinkedEvidence: links,
		}
		if len(links) == 0 {
			view.Verification = reportFindingUnverified
			view.UnverifiedRationale = "no linked artifact/log evidence"
		}
		out = append(out, view)
	}
	return out
}

func buildTaskNarratives(plan RunPlan, events []EventEnvelope, artifacts []artifactRecord, findings []reportFinding) []reportTaskNarrative {
	order := make([]string, 0, len(plan.Tasks))
	byTask := map[string]*reportTaskNarrative{}

	ensure := func(taskID string) *reportTaskNarrative {
		taskID = strings.TrimSpace(taskID)
		if taskID == "" {
			return nil
		}
		if existing, ok := byTask[taskID]; ok {
			return existing
		}
		view := &reportTaskNarrative{TaskID: taskID}
		byTask[taskID] = view
		order = append(order, taskID)
		return view
	}

	for _, task := range plan.Tasks {
		view := ensure(task.TaskID)
		if view == nil {
			continue
		}
		view.Title = strings.TrimSpace(task.Title)
		view.Goal = strings.TrimSpace(task.Goal)
		view.DependsOn = append([]string{}, task.DependsOn...)
		view.RiskLevel = strings.TrimSpace(task.RiskLevel)
		view.Strategy = strings.TrimSpace(task.Strategy)
		view.ExpectedArtifacts = append([]string{}, task.ExpectedArtifacts...)
		view.LastCommand = summarizeTaskSpecAction(task.Action)
	}

	for _, item := range artifacts {
		taskID := strings.TrimSpace(item.Artifact.TaskID)
		if taskID == "" {
			continue
		}
		view := ensure(taskID)
		if view == nil {
			continue
		}
		if path := strings.TrimSpace(item.Artifact.Path); path != "" {
			view.ArtifactPaths = append(view.ArtifactPaths, path)
		}
	}

	for _, event := range events {
		taskID := strings.TrimSpace(event.TaskID)
		if taskID == "" {
			continue
		}
		view := ensure(taskID)
		if view == nil {
			continue
		}
		payload := eventPayloadMap(event.Payload)
		switch event.Type {
		case EventTypeTaskStarted:
			view.Started = true
			if view.StartedAt.IsZero() || event.TS.Before(view.StartedAt) {
				view.StartedAt = event.TS
			}
			if worker := strings.TrimSpace(firstNonEmpty(asString(payload["worker_id"]), event.WorkerID)); worker != "" {
				view.WorkerID = worker
			}
			view.Attempt = maxInt(view.Attempt, asInt(payload["attempt"]))
			if view.Goal == "" {
				view.Goal = strings.TrimSpace(asString(payload["goal"]))
			}
		case EventTypeTaskProgress:
			message := normalizeProgressMessage(asString(payload["message"]))
			if message != "" {
				view.ProgressMessages = appendUniqueLimited(view.ProgressMessages, message, reportMaxProgressItems)
			}
			commandLine := summarizeTaskPayloadCommand(payload)
			if commandLine != "" {
				view.LastCommand = commandLine
			}
		case EventTypeTaskArtifact:
			if path := strings.TrimSpace(asString(payload["path"])); path != "" {
				view.ArtifactPaths = append(view.ArtifactPaths, path)
			}
			if commandLine := summarizeTaskPayloadCommand(payload); commandLine != "" {
				view.LastCommand = commandLine
			}
		case EventTypeTaskCompleted:
			view.Started = true
			view.Completed = true
			view.Failed = false
			if view.StartedAt.IsZero() {
				view.StartedAt = event.TS
			}
			view.TransitionAt = event.TS
			view.Attempt = maxInt(view.Attempt, asInt(payload["attempt"]))
			if worker := strings.TrimSpace(asString(payload["worker_id"])); worker != "" {
				view.WorkerID = worker
			}
			view.CompletionReason = strings.TrimSpace(asString(payload["reason"]))
			if contract, ok := payload["completion_contract"].(map[string]any); ok {
				view.ProducedArtifacts = append(view.ProducedArtifacts, asStringSlice(contract["produced_artifacts"])...)
			}
		case EventTypeTaskFailed:
			view.Started = true
			view.Failed = true
			if view.StartedAt.IsZero() {
				view.StartedAt = event.TS
			}
			view.TransitionAt = event.TS
			view.Attempt = maxInt(view.Attempt, asInt(payload["attempt"]))
			if worker := strings.TrimSpace(asString(payload["worker_id"])); worker != "" {
				view.WorkerID = worker
			}
			view.FailureReason = strings.TrimSpace(asString(payload["reason"]))
			view.FailureError = strings.TrimSpace(asString(payload["error"]))
		}
	}

	for _, findingView := range findings {
		taskID := strings.TrimSpace(findingView.Finding.TaskID)
		if taskID == "" {
			continue
		}
		view := ensure(taskID)
		if view == nil {
			continue
		}
		summary := fmt.Sprintf("[%s] %s", findingView.Verification, strings.TrimSpace(findingView.Finding.Title))
		if findingType := strings.TrimSpace(findingView.Finding.FindingType); findingType != "" {
			summary += fmt.Sprintf(" (`%s`)", findingType)
		}
		view.FindingSummaries = appendUniqueLimited(view.FindingSummaries, summary, reportMaxTaskFindings)
	}

	out := make([]reportTaskNarrative, 0, len(order))
	for _, taskID := range order {
		view := byTask[taskID]
		if view == nil {
			continue
		}
		view.DependsOn = dedupeTrimmed(view.DependsOn)
		view.ProducedArtifacts = dedupeTrimmed(view.ProducedArtifacts)
		view.ArtifactPaths = dedupeTrimmed(view.ArtifactPaths)
		view.ProgressMessages = dedupeTrimmed(view.ProgressMessages)
		view.FindingSummaries = dedupeTrimmed(view.FindingSummaries)
		out = append(out, *view)
	}
	return out
}

func summarizeNarrativeTaskStates(tasks []reportTaskNarrative) (completed, failed, started int) {
	for _, task := range tasks {
		if task.Started {
			started++
		}
		if task.Completed {
			completed++
		} else if task.Failed {
			failed++
		}
	}
	return completed, failed, started
}

func runWindow(events []EventEnvelope) (time.Time, time.Time, bool) {
	var start time.Time
	var end time.Time
	for _, event := range events {
		switch event.Type {
		case EventTypeRunStarted:
			if start.IsZero() || event.TS.Before(start) {
				start = event.TS
			}
		case EventTypeRunCompleted, EventTypeRunStopped:
			if end.IsZero() || event.TS.After(end) {
				end = event.TS
			}
		}
	}
	if start.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	if end.IsZero() {
		end = start
	}
	return start, end, true
}

func formatRuntimeWindow(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "unknown"
	}
	if end.Before(start) {
		return formatDuration(start.Sub(end))
	}
	return formatDuration(end.Sub(start))
}

func narrativeTaskOutcome(task reportTaskNarrative) string {
	switch {
	case task.Completed:
		if reason := strings.TrimSpace(task.CompletionReason); reason != "" {
			return "completed (" + reason + ")"
		}
		return "completed"
	case task.Failed:
		reason := strings.TrimSpace(task.FailureReason)
		if reason == "" {
			reason = "failed"
		}
		if errText := strings.TrimSpace(task.FailureError); errText != "" {
			return reason + ": " + truncateText(errText, 140)
		}
		return reason
	case task.Started:
		return "started (no terminal event)"
	default:
		return "not started"
	}
}

func reportTaskArtifacts(task reportTaskNarrative) []string {
	primary := make([]string, 0, len(task.ProducedArtifacts)+len(task.ArtifactPaths))
	for _, path := range task.ProducedArtifacts {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		primary = append(primary, path)
	}
	if len(primary) == 0 {
		for _, path := range task.ArtifactPaths {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			primary = append(primary, path)
		}
	}
	primary = dedupeTrimmed(primary)
	filtered := make([]string, 0, len(primary))
	for _, path := range primary {
		base := strings.ToLower(filepath.Base(path))
		if base == "resolved_target.json" {
			continue
		}
		filtered = append(filtered, path)
	}
	if len(filtered) == 0 {
		filtered = primary
	}
	return truncateWithOverflow(filtered, reportMaxTaskArtifacts)
}

func reportTaskTitle(taskID, title, goal string) string {
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(goal); trimmed != "" {
		return trimmed
	}
	return taskID
}

func summarizeTaskSpecAction(action TaskAction) string {
	switch strings.ToLower(strings.TrimSpace(action.Type)) {
	case "assist":
		prompt := truncateText(strings.TrimSpace(action.Prompt), reportCommandPreviewMax)
		if prompt == "" {
			return "assist workflow"
		}
		return "assist: " + prompt
	case "shell":
		return summarizeCommandLine("bash", []string{"-lc", action.Command}, reportCommandPreviewMax)
	default:
		if strings.TrimSpace(action.Command) == "" {
			return ""
		}
		return summarizeCommandLine(action.Command, action.Args, reportCommandPreviewMax)
	}
}

func summarizeTaskPayloadCommand(payload map[string]any) string {
	command := strings.TrimSpace(asString(payload["command"]))
	if command == "" {
		return ""
	}
	args := asStringSlice(payload["args"])
	return summarizeCommandLine(command, args, reportCommandPreviewMax)
}

func summarizeCommandLine(command string, args []string, maxLen int) string {
	parts := []string{strings.TrimSpace(command)}
	for _, arg := range args {
		if trimmed := strings.TrimSpace(arg); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	line := strings.TrimSpace(strings.Join(parts, " "))
	if line == "" {
		return ""
	}
	line = strings.Join(strings.Fields(line), " ")
	return truncateText(line, maxLen)
}

func normalizeProgressMessage(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	return strings.Join(strings.Fields(text), " ")
}

func appendUniqueLimited(values []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return values
		}
	}
	values = append(values, value)
	if limit > 0 && len(values) > limit {
		return values[:limit]
	}
	return values
}

func eventPayloadMap(raw json.RawMessage) map[string]any {
	payload := map[string]any{}
	if len(raw) == 0 {
		return payload
	}
	_ = json.Unmarshal(raw, &payload)
	return payload
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func asInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if v, err := typed.Int64(); err == nil {
			return int(v)
		}
		return 0
	case string:
		v, _ := strconv.Atoi(strings.TrimSpace(typed))
		return v
	default:
		return 0
	}
}

func asStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(asString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nonEmptyOrFallback(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

func truncateText(value string, maxLen int) string {
	if maxLen <= 0 {
		return strings.TrimSpace(value)
	}
	text := strings.TrimSpace(value)
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return strings.TrimSpace(text[:maxLen-3]) + "..."
}

func truncateWithOverflow(values []string, max int) []string {
	values = dedupeTrimmed(values)
	if max <= 0 || len(values) <= max {
		return values
	}
	return values[:max]
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = -duration
	}
	if duration < time.Second {
		return duration.Round(time.Millisecond).String()
	}
	return duration.Round(time.Second).String()
}

func renderReportPath(path, runRoot string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if !filepath.IsAbs(trimmed) || strings.TrimSpace(runRoot) == "" {
		return trimmed
	}
	root := filepath.Clean(runRoot)
	if !filepath.IsAbs(root) {
		if absoluteRoot, err := filepath.Abs(root); err == nil {
			root = absoluteRoot
		}
	}
	rel, err := filepath.Rel(root, filepath.Clean(trimmed))
	if err != nil {
		return trimmed
	}
	if rel == "." || rel == "" || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return trimmed
	}
	return rel
}

func artifactLinksByTask(artifacts []artifactRecord) map[string][]string {
	byTask := map[string][]string{}
	for _, item := range artifacts {
		taskID := strings.TrimSpace(item.Artifact.TaskID)
		if taskID == "" {
			continue
		}
		links := byTask[taskID]
		if path := strings.TrimSpace(item.Artifact.Path); path != "" {
			links = append(links, path)
		}
		if record := strings.TrimSpace(item.RecordPath); record != "" {
			links = append(links, record)
		}
		byTask[taskID] = dedupeTrimmed(links)
	}
	return byTask
}

func evidencePathLike(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "://") {
		return text
	}
	if strings.ContainsAny(text, " \t") {
		return ""
	}
	if strings.Contains(text, "/") || strings.Contains(text, "\\") {
		if strings.Contains(lower, "/logs/") || strings.Contains(lower, "/artifact/") ||
			strings.Contains(lower, "\\logs\\") || strings.Contains(lower, "\\artifact\\") {
			return text
		}
		if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "./") || strings.HasPrefix(text, "../") || strings.Contains(text, "\\") {
			if ext := strings.ToLower(filepath.Ext(text)); ext != "" {
				return text
			}
		}
	}
	for _, ext := range []string{".log", ".txt", ".json", ".xml", ".html", ".md", ".pcap"} {
		if strings.HasSuffix(lower, ext) {
			return text
		}
	}
	return ""
}

func dedupeTrimmed(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (m *Manager) listArtifactRecords(runID string) ([]artifactRecord, error) {
	files, err := filepath.Glob(filepath.Join(BuildRunPaths(m.SessionsDir, runID).ArtifactDir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := make([]artifactRecord, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var artifact Artifact
		if err := json.Unmarshal(data, &artifact); err != nil {
			return nil, err
		}
		out = append(out, artifactRecord{
			Artifact:   artifact,
			RecordPath: file,
		})
	}
	return out, nil
}

func (m *Manager) ResolveRunReportPath(runID string) (string, bool, error) {
	if strings.TrimSpace(runID) == "" {
		return "", false, fmt.Errorf("run id is required")
	}
	defaultPath := filepath.Join(BuildRunPaths(m.SessionsDir, runID).Root, "report.md")
	path := defaultPath
	events, err := m.Events(runID, 0)
	if err != nil {
		return "", false, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != EventTypeRunReportGenerated {
			continue
		}
		payload := map[string]any{}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &payload)
		}
		if raw, ok := payload["path"].(string); ok && strings.TrimSpace(raw) != "" {
			path = strings.TrimSpace(raw)
			break
		}
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return path, false, nil
		}
		return path, false, statErr
	}
	if info.IsDir() {
		return path, false, nil
	}
	return path, true, nil
}
