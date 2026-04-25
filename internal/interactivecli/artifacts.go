package interactivecli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
)

type transcriptRecord struct {
	At       string `json:"at"`
	Role     string `json:"role"`
	Content  string `json:"content"`
	Goal     string `json:"goal,omitempty"`
	TaskMode string `json:"task_mode,omitempty"`
}

type eventRecord struct {
	At           string `json:"at"`
	Kind         string `json:"kind"`
	Message      string `json:"message,omitempty"`
	Goal         string `json:"goal,omitempty"`
	TaskState    string `json:"task_state,omitempty"`
	ActiveStep   string `json:"active_step,omitempty"`
	Action       string `json:"action,omitempty"`
	Assessment   string `json:"assessment,omitempty"`
	FailureClass string `json:"failure_class,omitempty"`
}

type artifactRecorder struct {
	eventsPath     string
	transcriptPath string
}

func newArtifactRecorder(statePath string) artifactRecorder {
	root := filepath.Dir(strings.TrimSpace(statePath))
	return artifactRecorder{
		eventsPath:     filepath.Join(root, "events.ndjson"),
		transcriptPath: filepath.Join(root, "transcript.ndjson"),
	}
}

func (r artifactRecorder) recordTranscript(role, content string, packet ctxpacket.WorkerPacket) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return appendNDJSON(r.transcriptPath, transcriptRecord{
		At:       time.Now().UTC().Format(time.RFC3339Nano),
		Role:     strings.TrimSpace(role),
		Content:  content,
		Goal:     strings.TrimSpace(packet.SessionFoundation.Goal),
		TaskMode: strings.TrimSpace(packet.OperatorState.ModeHint),
	})
}

func (r artifactRecorder) recordEvent(kind, message string, packet ctxpacket.WorkerPacket) error {
	return appendNDJSON(r.eventsPath, eventRecord{
		At:           time.Now().UTC().Format(time.RFC3339Nano),
		Kind:         strings.TrimSpace(kind),
		Message:      strings.TrimSpace(message),
		Goal:         strings.TrimSpace(packet.SessionFoundation.Goal),
		TaskState:    strings.TrimSpace(packet.TaskRuntime.State),
		ActiveStep:   strings.TrimSpace(packet.PlanState.ActiveStep),
		Action:       strings.TrimSpace(packet.LatestExecutionResult.Action),
		Assessment:   strings.TrimSpace(packet.LatestExecutionResult.Assessment),
		FailureClass: strings.TrimSpace(packet.LatestExecutionResult.FailureClass),
	})
}

func (r artifactRecorder) recordProgress(progress runnerProgress) error {
	return appendNDJSON(r.eventsPath, eventRecord{
		At:           progress.Event.At.UTC().Format(time.RFC3339Nano),
		Kind:         "progress." + string(progress.Event.Kind),
		Message:      strings.TrimSpace(progress.Event.Message),
		Goal:         strings.TrimSpace(progress.Packet.SessionFoundation.Goal),
		TaskState:    strings.TrimSpace(progress.Packet.TaskRuntime.State),
		ActiveStep:   strings.TrimSpace(progress.Packet.PlanState.ActiveStep),
		Action:       strings.TrimSpace(progress.Event.Action),
		Assessment:   strings.TrimSpace(progress.Event.Assessment),
		FailureClass: strings.TrimSpace(progress.Event.FailureClass),
	})
}

func appendNDJSON(path string, v any) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(v)
}

func artifactEventMessageForClassification(decision string, err error) string {
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(decision)
}

func artifactEventMessageForOutcome(summary string, err error) string {
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(summary)
}

func artifactEventKindForOutcome(packet ctxpacket.WorkerPacket, err error) string {
	if err != nil {
		return "run.error"
	}
	switch strings.TrimSpace(packet.TaskRuntime.State) {
	case "done":
		return "run.completed"
	case "blocked":
		return "run.blocked"
	default:
		return "run.updated"
	}
}

func artifactEventMessageForProgress(progress workerloop.ProgressEvent) string {
	if strings.TrimSpace(progress.Message) != "" {
		return strings.TrimSpace(progress.Message)
	}
	if strings.TrimSpace(progress.Action) != "" {
		return strings.TrimSpace(progress.Action)
	}
	return string(progress.Kind)
}
