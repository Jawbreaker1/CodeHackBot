package assessment

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Jawbreaker1/CodeHackBot/internal/llmclient"
)

type Usage struct {
	Calls             int `json:"calls"`
	FailedCalls       int `json:"failed_calls"`
	ReportedTokens    int `json:"reported_tokens"`
	CallsWithoutUsage int `json:"calls_without_usage"`
}

// ModelBudget accounts for every model request in one assessment, including
// operator conversation while workers are active. It is safe to share between
// the coordinator and UI adapter.
type ModelBudget struct {
	mu       sync.Mutex
	idle     *sync.Cond
	limit    int
	usage    Usage
	inFlight int
	closed   bool
}

func NewModelBudget(limit int, usage Usage) *ModelBudget {
	budget := &ModelBudget{limit: limit, usage: usage}
	budget.idle = sync.NewCond(&budget.mu)
	return budget
}

func (m *ModelBudget) Limit() int { return m.limit }

// Client adds the assessment budget to a copy of the provider client. Callers
// keep their original client for sessions outside this assessment.
func (m *ModelBudget) Client(client llmclient.Client) llmclient.Client {
	before, after := client.BeforeRequest, client.OnCompletion
	client.BeforeRequest = func(ctx context.Context) error {
		if before != nil {
			if err := before(ctx); err != nil {
				return err
			}
		}
		return m.reserve(ctx)
	}
	client.OnCompletion = func(done llmclient.Completion, err error) {
		m.record(done, err)
		if after != nil {
			after(done, err)
		}
	}
	return client
}

func (m *ModelBudget) reserve(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("assessment model-call budget is closed")
	}
	if m.usage.Calls >= m.limit {
		return fmt.Errorf("assessment model-call budget exhausted (%d)", m.limit)
	}
	m.usage.Calls++
	m.inFlight++
	return nil
}

func (m *ModelBudget) record(c llmclient.Completion, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer func() {
		m.inFlight--
		if m.inFlight == 0 {
			m.idle.Broadcast()
		}
	}()
	if err != nil {
		m.usage.FailedCalls++
	}
	var usage struct {
		Total *int `json:"total_tokens"`
	}
	if json.Unmarshal(c.Usage, &usage) != nil || usage.Total == nil {
		m.usage.CallsWithoutUsage++
		return
	}
	m.usage.ReportedTokens += *usage.Total
}

func (m *ModelBudget) Usage() Usage { m.mu.Lock(); defer m.mu.Unlock(); return m.usage }

// CloseAndWait seals the budget before a final report is written, so a live
// conversation cannot finish after that report's usage snapshot.
func (m *ModelBudget) CloseAndWait() {
	m.mu.Lock()
	m.closed = true
	for m.inFlight > 0 {
		m.idle.Wait()
	}
	m.mu.Unlock()
}
