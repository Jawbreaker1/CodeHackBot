package assessment

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

func TestModelBudgetIsSharedAcrossClientsAndAccountsForFailures(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 2 {
			http.Error(w, "fixture failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ready"}}],"usage":{"total_tokens":7}}`))
	}))
	defer provider.Close()
	budget := NewModelBudget(2, Usage{})
	base := llmclient.Client{BaseURL: provider.URL + "/v1", Model: "fixture"}
	planning := budget.Client(base)
	chat := budget.Client(base)
	messages := []llmclient.Message{{Role: "user", Content: "fixture"}}
	if _, err := planning.Chat(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	if _, err := chat.Chat(context.Background(), messages); err == nil {
		t.Fatal("provider failure was hidden")
	}
	if _, err := planning.Chat(context.Background(), messages); err == nil || !strings.Contains(err.Error(), "budget exhausted") {
		t.Fatalf("third shared request should be denied: %v", err)
	}
	if requests != 2 || budget.Usage() != (Usage{Calls: 2, FailedCalls: 1, ReportedTokens: 7, CallsWithoutUsage: 1}) {
		t.Fatalf("shared budget: requests=%d usage=%+v", requests, budget.Usage())
	}
}

func TestModelBudgetReservesConcurrentRequestsAcrossRoles(t *testing.T) {
	arrived := make(chan struct{}, 3)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ready"}}]}`))
	}))
	defer provider.Close()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	budget := NewModelBudget(4, Usage{})
	base := llmclient.Client{BaseURL: provider.URL + "/v1", Model: "fixture"}
	roles := []llmclient.Client{budget.Client(base), budget.Client(base)}
	results := make(chan error, 3)
	for _, role := range roles {
		go func(client llmclient.Client) {
			_, err := client.Chat(context.Background(), []llmclient.Message{{Role: "user", Content: "fixture"}})
			results <- err
		}(role)
	}
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent requests did not reach the provider")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	canceled := make(chan error, 1)
	go func() {
		_, err := roles[0].Chat(ctx, []llmclient.Message{{Role: "user", Content: "cancel while queued"}})
		canceled <- err
	}()
	if err := <-canceled; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued request did not honor cancellation: %v", err)
	}
	if got := budget.Usage().Calls; got != 2 {
		t.Fatalf("queued request consumed model budget before provider entry: %d", got)
	}
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := roles[0].Chat(context.Background(), []llmclient.Message{{Role: "user", Content: "third"}})
		results <- err
	}()
	<-started
	select {
	case <-arrived:
		t.Fatal("third request reached the provider while two were active")
	case <-time.After(30 * time.Millisecond):
	}
	if got := budget.Usage().Calls; got != 2 {
		t.Fatalf("third request was reserved before a slot opened: %d", got)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("queued third request never reached the provider")
	}
	for range 3 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := budget.Usage(); got.Calls != 3 || got.CallsWithoutUsage != 3 {
		t.Fatalf("concurrent usage = %+v", got)
	}
}
