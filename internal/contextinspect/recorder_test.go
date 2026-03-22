package contextinspect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestRecorderCapture(t *testing.T) {
	dir := t.TempDir()
	recorder := Recorder{Dir: dir}
	packet := ctxpacket.WorkerPacket{
		BehaviorFrame:     behavior.Frame{SystemPrompt: "prompt", AgentsText: "agents", RuntimeMode: "worker"},
		SessionFoundation: session.Foundation{Goal: "test goal", ReportingRequirement: "owasp"},
		RunningSummary:    "summary",
	}
	if err := recorder.Capture(1, "pre-llm", packet); err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	path := filepath.Join(dir, "step-001-pre-llm.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "[session_foundation]") || !strings.Contains(text, "test goal") {
		t.Fatalf("snapshot missing expected content:\n%s", text)
	}

	metaPath := filepath.Join(dir, "step-001-pre-llm-meta.txt")
	meta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("ReadFile(meta) error = %v", err)
	}
	metaText := string(meta)
	for _, want := range []string{
		"[snapshot]",
		"total_chars:",
		"approx_total_tokens:",
		"section_count:",
		"[sections]",
		"behavior_frame: chars=",
		"approx_tokens=",
		"session_foundation: chars=",
	} {
		if !strings.Contains(metaText, want) {
			t.Fatalf("meta snapshot missing %q in:\n%s", want, metaText)
		}
	}
}
