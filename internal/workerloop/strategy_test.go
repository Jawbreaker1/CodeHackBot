package workerloop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/session"
)

func TestSelectedStrategySurvivesLaterWorkerContext(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "strategies")
	if err := os.MkdirAll(filepath.Join(dir, "credential-recovery"), 0700); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(dir, "catalog.md")
	if err := os.WriteFile(catalog, []byte("credential-recovery/SKILL.md"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credential-recovery", "SKILL.md"), []byte("Revision: 1\nTry common numeric suffix mutations after a plain dictionary miss."), 0600); err != nil {
		t.Fatal(err)
	}
	packet := ctxpacket.NewInitialWorkerPacket(behavior.Frame{SystemPrompt: "system", AgentsText: "rules", StrategyCatalogPath: catalog, StrategyCatalogText: "credential-recovery/SKILL.md"}, session.Foundation{Goal: "recover local artifact"}, root, "fixture", "per_action", 10)
	loaded, err := loadStrategy(&packet, "credential-recovery/SKILL.md")
	if err != nil || !loaded || len(packet.StrategyGuidance) != 1 || len(packet.StrategyGuidance[0].SHA256) != 64 {
		t.Fatalf("strategy load = %v, %v, %+v", loaded, err, packet.StrategyGuidance)
	}
	packet.RelevantRecentResults = []ctxpacket.ExecutionResult{{OutputEvidence: strings.Repeat("older tool output ", 1200)}}
	view, err := packet.ModelView(20000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(view.RenderWithoutBehaviorFrame(), "numeric suffix mutations") {
		t.Fatal("selected guide disappeared from later worker context")
	}
	loaded, err = loadStrategy(&packet, "credential-recovery/SKILL.md")
	if err != nil || loaded || len(packet.StrategyGuidance) != 1 {
		t.Fatal("repeated lookup duplicated strategy guidance")
	}
	if _, err := loadStrategy(&packet, "../outside.md"); err == nil {
		t.Fatal("path traversal escaped the strategy catalog")
	}
}

func TestLoadStrategyDecisionHasNoExecutionFields(t *testing.T) {
	if _, err := ParseResponse(`{"type":"load_strategy","strategy":"credential-recovery/SKILL.md"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseResponse(`{"type":"load_strategy","strategy":"credential-recovery/SKILL.md","command":"cat /etc/passwd"}`); err == nil {
		t.Fatal("load_strategy accepted execution fields")
	}
}
