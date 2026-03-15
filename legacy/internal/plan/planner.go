package plan

import (
	"context"
	"fmt"
	"strings"
)

type Input struct {
	SessionID  string
	Scope      []string
	Targets    []string
	Summary    string
	KnownFacts []string
	Inventory  string
	Plan       string
	Goal       string
	Playbooks  string
}

type Planner interface {
	Plan(ctx context.Context, input Input) (string, error)
	Next(ctx context.Context, input Input) ([]string, error)
}

type ChainedPlanner struct {
	Primary  Planner
	Fallback Planner
}

func (c ChainedPlanner) Plan(ctx context.Context, input Input) (string, error) {
	if c.Primary != nil {
		content, err := c.Primary.Plan(ctx, input)
		if err == nil && strings.TrimSpace(content) != "" {
			return content, nil
		}
	}
	if c.Fallback != nil {
		return c.Fallback.Plan(ctx, input)
	}
	return "", fmt.Errorf("no planner available")
}

func (c ChainedPlanner) Next(ctx context.Context, input Input) ([]string, error) {
	if c.Primary != nil {
		steps, err := c.Primary.Next(ctx, input)
		if err == nil && len(steps) > 0 {
			return steps, nil
		}
	}
	if c.Fallback != nil {
		return c.Fallback.Next(ctx, input)
	}
	return nil, fmt.Errorf("no planner available")
}

type FallbackPlanner struct{}

func (FallbackPlanner) Plan(_ context.Context, input Input) (string, error) {
	builder := strings.Builder{}
	builder.WriteString("## Plan (Fallback)\n\n")
	builder.WriteString("### Scope\n")
	builder.WriteString(fmt.Sprintf("- %s\n", joinOrFallback(input.Scope)))
	builder.WriteString("\n### Targets\n")
	builder.WriteString(fmt.Sprintf("- %s\n\n", joinOrFallback(input.Targets)))
	builder.WriteString("### Phases\n")
	builder.WriteString("- Recon: validate scope, identify exposed services\n")
	builder.WriteString("- Enumeration: map ports, versions, and likely attack surface\n")
	builder.WriteString("- Validation: confirm findings with safe checks\n")
	builder.WriteString("- Escalation: attempt controlled privilege validation if needed\n")
	builder.WriteString("- Reporting: capture evidence and remediation guidance\n")
	builder.WriteString("\n### Risks/Notes\n")
	builder.WriteString(fmt.Sprintf("- Session: %s\n", fallbackValue(input.SessionID, "unknown")))
	if len(input.KnownFacts) > 0 {
		builder.WriteString("\n### Known Facts\n")
		for _, fact := range input.KnownFacts {
			builder.WriteString("- " + fact + "\n")
		}
	}
	if input.Playbooks != "" {
		builder.WriteString("\n### Relevant Playbooks\n")
		builder.WriteString(input.Playbooks + "\n")
	}
	return builder.String(), nil
}

func (FallbackPlanner) Next(_ context.Context, input Input) ([]string, error) {
	steps := []string{}
	if len(input.Targets) > 0 {
		steps = append(steps, fmt.Sprintf("Validate scope for %s and run a safe recon scan.", input.Targets[0]))
	} else {
		steps = append(steps, "Clarify targets and scope before running recon scans.")
	}
	steps = append(steps, "Review recent logs for open ports or service banners.")
	steps = append(steps, "Update plan with confirmed findings.")
	return steps, nil
}

func joinOrFallback(values []string) string {
	if len(values) == 0 {
		return "not specified"
	}
	return strings.Join(values, ", ")
}

func fallbackValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
