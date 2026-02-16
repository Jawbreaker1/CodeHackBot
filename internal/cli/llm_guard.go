package cli

import (
	"context"
	"fmt"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
	"github.com/Jawbreaker1/CodeHackBot/internal/plan"
)

type guardedSummarizer struct {
	allow     func() bool
	onSuccess func()
	onFailure func(error)
	primary   memory.Summarizer
	fallback  memory.Summarizer
}

func (g guardedSummarizer) Summarize(ctx context.Context, input memory.SummaryInput) (memory.SummaryOutput, error) {
	if g.allow != nil && !g.allow() {
		return g.fallback.Summarize(ctx, input)
	}
	output, err := g.primary.Summarize(ctx, input)
	if err != nil {
		if g.onFailure != nil {
			g.onFailure(err)
		}
		if g.fallback != nil {
			return g.fallback.Summarize(ctx, input)
		}
		return output, err
	}
	if g.onSuccess != nil {
		g.onSuccess()
	}
	return output, nil
}

type guardedPlanner struct {
	allow     func() bool
	onSuccess func()
	onFailure func(error)
	primary   plan.Planner
	fallback  plan.Planner
}

func (g guardedPlanner) Plan(ctx context.Context, input plan.Input) (string, error) {
	if g.allow != nil && !g.allow() {
		return g.fallback.Plan(ctx, input)
	}
	content, err := g.primary.Plan(ctx, input)
	if err != nil {
		if g.onFailure != nil {
			g.onFailure(err)
		}
		if g.fallback != nil {
			return g.fallback.Plan(ctx, input)
		}
		return "", err
	}
	if g.onSuccess != nil {
		g.onSuccess()
	}
	return content, nil
}

func (g guardedPlanner) Next(ctx context.Context, input plan.Input) ([]string, error) {
	if g.allow != nil && !g.allow() {
		return g.fallback.Next(ctx, input)
	}
	steps, err := g.primary.Next(ctx, input)
	if err != nil {
		if g.onFailure != nil {
			g.onFailure(err)
		}
		if g.fallback != nil {
			return g.fallback.Next(ctx, input)
		}
		return nil, err
	}
	if g.onSuccess != nil {
		g.onSuccess()
	}
	return steps, nil
}

type guardedAssistant struct {
	allow     func() bool
	onSuccess func()
	onFailure func(error)
	onFallback func(error)
	primary   assist.Assistant
	fallback  assist.Assistant
}

func (g guardedAssistant) Suggest(ctx context.Context, input assist.Input) (assist.Suggestion, error) {
	if g.allow != nil && !g.allow() {
		return g.fallback.Suggest(ctx, input)
	}
	suggestion, err := g.primary.Suggest(ctx, input)
	if err != nil {
		if g.onFailure != nil {
			g.onFailure(err)
		}
		if g.fallback != nil {
			if g.onFallback != nil {
				g.onFallback(err)
			}
			return g.fallback.Suggest(ctx, input)
		}
		return assist.Suggestion{}, err
	}
	if suggestion.Type == "" {
		err = fmt.Errorf("empty suggestion type")
		if g.onFailure != nil {
			g.onFailure(err)
		}
		if g.fallback != nil {
			if g.onFallback != nil {
				g.onFallback(err)
			}
			return g.fallback.Suggest(ctx, input)
		}
		return assist.Suggestion{}, err
	}
	if g.onSuccess != nil {
		g.onSuccess()
	}
	return suggestion, nil
}
