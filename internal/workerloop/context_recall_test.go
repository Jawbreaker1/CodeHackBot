package workerloop

import (
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
