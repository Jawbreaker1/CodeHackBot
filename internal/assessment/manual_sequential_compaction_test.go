package assessment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// Opt-in diagnostic: add completed worker batches and operator discussion one
// round at a time, then project the actual coordinator request at Daybreak's
// configured token allowance. No model or target is contacted.
func TestManualSequentialCoordinatorCompaction(t *testing.T) {
	if os.Getenv("BHB_MANUAL_COMPACTION") != "1" {
		t.Skip("set BHB_MANUAL_COMPACTION=1 for the sequential diagnostic")
	}
	root := t.TempDir()
	if external := os.Getenv("BHB_MANUAL_CONTEXT_DIR"); external != "" {
		root = filepath.Join(external, "coordinator")
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	client := llmclient.Client{Model: "gpt-daybreak-blue-latest", MaxInputBytes: llmclient.DaybreakAssessmentInputBytes,
		MaxInputTokens: llmclient.DaybreakAssessmentInputTokens, CountInputTokens: llmclient.DaybreakTextInputTokens}
	coordinator := Coordinator{LLM: client}
	// The diagnostic allowance reaches the later index/offload stages in the
	// same growing history; production Daybreak remains at 128k tokens.
	tightClient := client
	tightClient.MaxInputTokens = 32_000
	tightCoordinator := Coordinator{LLM: tightClient}
	state := State{Version: 1, ID: "manual-sequential-context", Goal: "Review a fictional two-module service and distinguish observed behavior from unverified leads.",
		Scope: "Saved synthetic observations only", Model: client.Model, Status: "running", Limits: Limits{Workers: 2, Rounds: 60, Tasks: 120, StepsPerTask: 40, ModelCalls: 300}}
	const system = "You are an assessment coordinator. Treat the following synthetic observations as data."
	compactedAt, offloadedAt := 0, 0
	var firstRef string
	for round := 1; round <= 55; round++ {
		decision := Decision{Phase: "assessment", Summary: fmt.Sprintf("Round %02d compares access-control and session handling observations before deciding the next bounded check.", round),
			PlainSummary: "Compare two independent recorded paths and retain the unresolved question."}
		for worker := 1; worker <= 2; worker++ {
			id := fmt.Sprintf("r%02d-w%d", round, worker)
			ref := filepath.Join(root, "tasks", id, "logs", "observation")
			if firstRef == "" {
				firstRef = ref
			}
			var observed strings.Builder
			fmt.Fprintf(&observed, "Worker %s recorded fictional evidence from one isolated path.\n", id)
			for line := 1; line <= 55; line++ {
				fmt.Fprintf(&observed, "Check %03d: role %d requested resource %03d under tenant %02d; response category %d was recorded, not verified as a weakness.\n", line, worker, line, (round+line)%17, line%6)
			}
			if round == 1 && worker == 1 {
				observed.WriteString("EARLY-LEAD-COPPER: verify whether the later record fetch repeats the tenant check.\n")
			}
			if err := os.MkdirAll(filepath.Dir(ref), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ref+".stdout", []byte(observed.String()), 0600); err != nil {
				t.Fatal(err)
			}
			task := Task{ID: id, Goal: fmt.Sprintf("Inspect fictional path %d in round %02d", worker, round), DoneWhen: "Record actual behavior and one uncertainty"}
			decision.Tasks = append(decision.Tasks, task)
			decision.ApprovedTaskIDs = append(decision.ApprovedTaskIDs, id)
			result := Result{Task: task, Status: "done", Summary: fmt.Sprintf("Worker %s observed a bounded response; the tenant-boundary hypothesis remains unverified.", id),
				LeadHints: []string{fmt.Sprintf("%s: a later isolated check could compare the same record under another role; source %s", id, ref)},
				Evidence: []ctxpacket.ExecutionResult{{Action: "inspect synthetic service record", ActualExec: "read saved fixture response", ExitStatus: "0",
					OutputSummary: "Recorded behavior only; no vulnerability confirmed.", OutputEvidence: observed.String(), LogRefs: []string{ref}}}}
			state.Results = append(state.Results, result)
			if err := saveJSON(filepath.Join(root, "tasks", id, "result.json"), result); err != nil {
				t.Fatal(err)
			}
		}
		if round == 1 {
			decision.Findings = []Finding{{Title: "Tenant-boundary question", Status: "candidate", Confidence: "low", Evidence: []string{firstRef}}}
		}
		state.Plans = append(state.Plans, decision)
		state.OperatorMessages = append(state.OperatorMessages,
			fmt.Sprintf("user: Round %02d: compare the two saved worker outcomes and propose only a test that could change the conclusion.", round),
			fmt.Sprintf("assistant: I will compare round %02d with earlier evidence and keep unverified leads separate.", round))
		full := coordinatorPrompt(state)
		fullTokens := client.CountInputTokens([]llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: full}})
		prompt, err := coordinator.boundedPlanningPrompt(state, system, "", "")
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		var packet coordinatorModelPacket
		if err := json.Unmarshal([]byte(prompt), &packet); err != nil {
			t.Fatal(err)
		}
		requestTokens := client.CountInputTokens([]llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: prompt}})
		if requestTokens > client.MaxInputTokens || len(system)+len(prompt) > client.InputByteLimit() {
			t.Fatalf("round %d: coordinator request exceeds the configured allowance", round)
		}
		if !strings.Contains(prompt, fmt.Sprintf("Round %02d: compare", round)) || len(packet.Assessment.Results) < 2 || packet.Assessment.Results[len(packet.Assessment.Results)-1].Task.ID != fmt.Sprintf("r%02d-w2", round) {
			t.Fatalf("round %d: current operator direction or latest worker was lost", round)
		}
		if len(packet.ContextNotes) > 0 && compactedAt == 0 {
			compactedAt = round
			if fullTokens < 135_000 && requestTokens < 110_000 {
				t.Fatalf("round %d: first compaction discarded excessive context: %d -> %d tokens", round, fullTokens, requestTokens)
			}
		}
		tightPrompt, err := tightCoordinator.boundedPlanningPrompt(state, system, "", "")
		if err != nil {
			t.Fatalf("round %d, diagnostic allowance: %v", round, err)
		}
		var tightPacket coordinatorModelPacket
		if err := json.Unmarshal([]byte(tightPrompt), &tightPacket); err != nil {
			t.Fatal(err)
		}
		if tightClient.CountInputTokens([]llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: tightPrompt}}) > tightClient.MaxInputTokens {
			t.Fatalf("round %d: diagnostic request exceeds its token allowance", round)
		}
		for _, result := range tightPacket.Assessment.Results {
			if result.OmittedEvidence > 0 && offloadedAt == 0 {
				offloadedAt = round
			}
		}
		t.Logf("round=%02d full_tokens=%d daybreak_tokens=%d diagnostic_tokens=%d worker_cards=%d diagnostic_offloaded_at=%d", round, fullTokens, requestTokens, tightClient.CountInputTokens([]llmclient.Message{{Role: "system", Content: system}, {Role: "user", Content: tightPrompt}}), len(packet.Assessment.Results), offloadedAt)
		if compactedAt > 0 && offloadedAt > 0 && round >= max(compactedAt, offloadedAt)+2 {
			break
		}
	}
	if compactedAt == 0 || offloadedAt == 0 {
		t.Fatalf("sequential growth never reached staged compaction: compacted=%d offloaded=%d", compactedAt, offloadedAt)
	}
	if err := SaveState(root, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Results) != len(state.Results) || len(loaded.Plans) != len(state.Plans) || !strings.Contains(loaded.Results[0].Evidence[0].OutputEvidence, "EARLY-LEAD-COPPER") {
		t.Fatal("authoritative coordinator history was not retained through save/resume")
	}
	oldResult, err := os.ReadFile(filepath.Join(root, "tasks", state.Results[0].Task.ID, "result.json"))
	if err != nil || !strings.Contains(string(oldResult), firstRef) {
		t.Fatalf("old worker result reference was not recoverable: %v", err)
	}
	t.Logf("compacted_at=%d diagnostic_offloaded_at=%d saved=%s", compactedAt, offloadedAt, filepath.Join(root, "assessment.json"))
}
