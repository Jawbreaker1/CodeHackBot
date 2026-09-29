package workerloop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
)

func TestRecallContextFindsOffloadedResultAndReadsRegisteredLog(t *testing.T) {
	logDir := t.TempDir()
	results := make([]ctxpacket.ExecutionResult, 0, 30)
	var oldRef string
	for i := 0; i < 30; i++ {
		ref := filepath.Join(logDir, "action-"+strings.Repeat("0", 2)+string(rune('a'+i)))
		if i == 29 {
			oldRef = ref
		}
		results = append(results, ctxpacket.ExecutionResult{Action: "inspect source", OutputSummary: "module detail", LogRefs: []string{ref}})
	}
	results[29].Action = "inspect old authorization branch"
	results[29].OutputSummary = "observed tenant boundary"
	const saved = "first saved observation\nsecond saved observation\n"
	if err := os.WriteFile(oldRef+".stdout", []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	packet := ctxpacket.WorkerPacket{RelevantRecentResults: results}
	const budget = 4500
	view, err := packet.ModelView(budget)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.RelevantRecentResults) >= 30 || len(view.RelevantRecentResults)+view.OffloadedResultCount != 30 || len(view.Render()) > budget {
		t.Fatal("tight model view did not offload older results")
	}
	loop := Loop{Executor: execx.Executor{LogDir: logDir}}
	index, err := loop.recallContext(packet, Response{Type: "recall_context", ContextQuery: "find the older command result showing tenant boundary in the authorization source"})
	if err != nil || !strings.HasPrefix(index.Content, "ref=\""+oldRef+"\"") {
		t.Fatalf("could not locate old result: %+v, %v", index, err)
	}
	chunk, err := loop.recallContext(packet, Response{Type: "recall_context", ContextRef: oldRef, ContextOffset: 6})
	if err != nil || chunk.Ref != oldRef || chunk.Content != saved[6:] || chunk.TotalBytes != int64(len(saved)) {
		t.Fatalf("could not rehydrate registered output: %+v, %v", chunk, err)
	}
	if _, err := loop.recallContext(packet, Response{Type: "recall_context", ContextRef: filepath.Join(logDir, "unrecorded")}); err == nil {
		t.Fatal("unregistered log path was accepted")
	}
	if refs := registeredContextRefs(packet, []string{oldRef, filepath.Join(logDir, "unrecorded")}); len(refs) != 1 || refs[0] != oldRef {
		t.Fatalf("pinned an unregistered result: %v", refs)
	}
}

func TestRecallContextRecoversOffloadedConversationAndPlan(t *testing.T) {
	packet := ctxpacket.WorkerPacket{}
	for i := 0; i < 60; i++ {
		packet.RecentConversation = append(packet.RecentConversation, fmt.Sprintf("User: routine-%02d %s", i, strings.Repeat("ordinary context ", 20)))
		packet.PlanHistory = append(packet.PlanHistory, ctxpacket.PlanRevision{Turn: i + 1, Plan: ctxpacket.PlanState{Summary: fmt.Sprintf("routine plan %02d", i)}})
	}
	const earlyOperator = "Operator answer: Preserve the original source labels and compare only the supplied records."
	packet.RecentConversation[2] = earlyOperator
	packet.PlanHistory[2].Plan.Summary = "Early plan: compare the original source labels"
	view, err := packet.ModelView(7000)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(view.Render(), earlyOperator) || view.OffloadedPlanCount == 0 {
		t.Fatal("fixture did not offload early conversation and plan content")
	}
	loop := Loop{Executor: execx.Executor{LogDir: t.TempDir()}}
	index, err := loop.recallContext(packet, Response{Type: "recall_context", ContextQuery: "original source labels"})
	if err != nil || !index.Matched || !strings.Contains(index.Content, `ref="conversation:2"`) || !strings.Contains(index.Content, `ref="plan:2"`) {
		t.Fatalf("saved context index did not find both sources: %+v, %v", index, err)
	}
	conversation, err := loop.recallContext(packet, Response{Type: "recall_context", ContextRef: "conversation:2"})
	if err != nil || conversation.Content != earlyOperator || conversation.NextOffset != int64(len(earlyOperator)) {
		t.Fatalf("operator direction could not be recovered exactly: %+v, %v", conversation, err)
	}
	plan, err := loop.recallContext(packet, Response{Type: "recall_context", ContextRef: "plan:2"})
	if err != nil || !strings.Contains(plan.Content, "Early plan: compare the original source labels") {
		t.Fatalf("saved plan could not be recovered: %+v, %v", plan, err)
	}
	if _, err := loop.recallContext(packet, Response{Type: "recall_context", ContextRef: "conversation:60"}); err == nil {
		t.Fatal("unregistered conversation reference was accepted")
	}
}

func TestRecallContextUTF8PagesHaveExactOffsets(t *testing.T) {
	logDir := t.TempDir()
	ref := filepath.Join(logDir, "text")
	saved := strings.Repeat("a", recallChunkBytes-1) + "å" + strings.Repeat("b", 20)
	if err := os.WriteFile(ref+".stdout", []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	packet := ctxpacket.WorkerPacket{LatestExecutionResult: ctxpacket.ExecutionResult{Action: "read saved text", LogRefs: []string{ref}}}
	loop := Loop{Executor: execx.Executor{LogDir: logDir}}
	first, err := loop.recallContext(packet, Response{Type: "recall_context", ContextRef: ref})
	if err != nil {
		t.Fatal(err)
	}
	second, err := loop.recallContext(packet, Response{Type: "recall_context", ContextRef: ref, ContextOffset: first.NextOffset})
	if err != nil {
		t.Fatal(err)
	}
	if first.Content+second.Content != saved || first.NextOffset != recallChunkBytes-1 || second.NextOffset != int64(len(saved)) {
		t.Fatalf("page boundary changed saved UTF-8 text: first=%+v second=%+v", first, second)
	}
	packet.RecentConversation = []string{"Operator answer: " + saved}
	first, err = loop.recallContext(packet, Response{Type: "recall_context", ContextRef: "conversation:0"})
	if err != nil {
		t.Fatal(err)
	}
	second, err = loop.recallContext(packet, Response{Type: "recall_context", ContextRef: "conversation:0", ContextOffset: first.NextOffset})
	if err != nil || first.Content+second.Content != packet.RecentConversation[0] {
		t.Fatalf("saved conversation pagination changed UTF-8 text: first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestRecallContextDecisionContract(t *testing.T) {
	if _, err := ParseResponse(`{"type":"recall_context","context_query":"authorization"}`); err != nil {
		t.Fatal(err)
	}
	clearPins, err := ParseResponse(`{"type":"recall_context","context_query":"authorization","context_keep_refs":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if clearPins.ContextKeepRefs == nil || len(*clearPins.ContextKeepRefs) != 0 {
		t.Fatal("empty context_keep_refs must clear previously pinned evidence")
	}
	for _, decision := range []string{
		`{"type":"recall_context"}`,
		`{"type":"recall_context","context_ref":"x","context_query":"y"}`,
		`{"type":"recall_context","context_ref":"x","context_offset":-1}`,
		`{"type":"bash","command":"true","context_ref":"x"}`,
	} {
		if _, err := ParseResponse(decision); err == nil {
			t.Fatalf("accepted invalid context decision %s", decision)
		}
	}
}
