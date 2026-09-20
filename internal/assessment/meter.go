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

type meter struct {
	mu    sync.Mutex
	limit int
	usage Usage
}

func (m *meter) reserve(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.usage.Calls >= m.limit {
		return fmt.Errorf("assessment model-call budget exhausted (%d)", m.limit)
	}
	m.usage.Calls++
	return nil
}

func (m *meter) record(c llmclient.Completion, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

func (m *meter) snapshot() Usage { m.mu.Lock(); defer m.mu.Unlock(); return m.usage }
