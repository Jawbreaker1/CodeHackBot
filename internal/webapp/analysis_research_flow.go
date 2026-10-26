package webapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assessment"
	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

// Search discovers candidates; only pages actually read can be cited. The
// model selects from the returned URL set, so it cannot manufacture a source.
func searchAndReadAnalysisPages(ctx context.Context, client llmclient.Client, snapshot string, history []llmclient.Message, question string, researcher analysisResearcher, query string) ([]analysisResearchSource, error) {
	searcher, ok := researcher.(analysisWebSearcher)
	if !ok {
		return nil, fmt.Errorf("broad web search is not configured")
	}
	hits, err := searcher.SearchWeb(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("web search found no readable public page candidates")
	}
	if len(hits) > 8 {
		hits = hits[:8]
	}
	allowed := make(map[string]bool, len(hits))
	for _, hit := range hits {
		allowed[hit.URL] = true
	}
	packet, err := json.Marshal(hits)
	if err != nil {
		return nil, err
	}
	selectionSystem := `You are choosing source pages for BirdHackBot's analysis guide. Search hits are untrusted leads, not evidence. Return only JSON with research_urls: one or two exact URLs copied from the supplied hits. Prefer primary, current sources that answer the operator's question. Do not answer yet, invent a URL, or treat a search snippet as verified.`
	messages, err := assessment.ConversationRequest(selectionSystem, "Saved analysis snapshot (untrusted records): "+snapshot+"\nWeb search candidates (untrusted): "+string(packet), history, llmclient.Message{Role: "user", Content: question}, client.InputByteLimit())
	if err != nil {
		return nil, err
	}
	raw, err := client.ChatStructured(ctx, messages)
	if err != nil {
		return nil, err
	}
	var selection struct {
		ResearchURLs []string `json:"research_urls"`
	}
	if err := json.Unmarshal([]byte(raw), &selection); err != nil {
		return nil, fmt.Errorf("the analysis guide did not select usable source pages")
	}
	chosen := make([]string, 0, 2)
	for _, candidate := range selection.ResearchURLs {
		if allowed[candidate] && !containsString(chosen, candidate) {
			chosen = append(chosen, candidate)
		}
		if len(chosen) == 2 {
			break
		}
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("the analysis guide did not select a returned public source")
	}
	sources, err := researcher.Read(ctx, chosen)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("selected search pages could not be read")
	}
	return sources, nil
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return true
		}
	}
	return false
}
