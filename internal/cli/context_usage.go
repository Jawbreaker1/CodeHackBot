package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

type usageCounter struct {
	Used int
	Cap  int
}

func (c usageCounter) percent() int {
	if c.Cap <= 0 {
		return -1
	}
	if c.Used <= 0 {
		return 0
	}
	pct := (c.Used*100 + c.Cap - 1) / c.Cap
	if pct > 100 {
		return 100
	}
	return pct
}

func (c usageCounter) remaining() int {
	if c.Cap <= 0 {
		return -1
	}
	remaining := c.Cap - c.Used
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (c usageCounter) label() string {
	if c.Cap <= 0 {
		return "off"
	}
	return fmt.Sprintf("%d/%d (%d%%)", c.Used, c.Cap, c.percent())
}

type contextUsage struct {
	Logs       usageCounter
	Observed   usageCounter
	Chat       usageCounter
	StepWindow usageCounter
	LogWindow  usageCounter

	BufferPercent    int
	SummarizePercent int
	OverallPercent   int
}

func buildContextUsage(cfg config.Config, state memory.State, chatLines int) contextUsage {
	logWindowCap := summarizeLogThreshold(cfg.Context.MaxRecentOutputs, cfg.Context.SummarizeAtPercent)

	usage := contextUsage{
		Logs:       usageCounter{Used: len(state.RecentLogs), Cap: cfg.Context.MaxRecentOutputs},
		Observed:   usageCounter{Used: len(state.RecentObservations), Cap: cfg.Context.MaxRecentOutputs},
		Chat:       usageCounter{Used: chatLines, Cap: cfg.Context.ChatHistoryLines},
		StepWindow: usageCounter{Used: state.StepsSinceSummary, Cap: cfg.Context.SummarizeEvery},
		LogWindow:  usageCounter{Used: len(state.RecentLogs), Cap: logWindowCap},
	}

	usage.BufferPercent = maxUsagePercent(usage.Logs.percent(), usage.Observed.percent(), usage.Chat.percent())
	usage.SummarizePercent = maxUsagePercent(usage.StepWindow.percent(), usage.LogWindow.percent())
	usage.OverallPercent = maxUsagePercent(usage.BufferPercent, usage.SummarizePercent)
	return usage
}

func summarizeLogThreshold(maxRecentOutputs, summarizeAtPercent int) int {
	if maxRecentOutputs <= 0 || summarizeAtPercent <= 0 {
		return 0
	}
	threshold := (maxRecentOutputs*summarizeAtPercent + 99) / 100
	if threshold < 1 {
		return 1
	}
	return threshold
}

func maxUsagePercent(values ...int) int {
	max := 0
	for _, value := range values {
		if value < 0 {
			continue
		}
		if value > max {
			max = value
		}
	}
	return max
}

func (u contextUsage) statusLine() string {
	if !u.enabled() {
		return "disabled"
	}
	return fmt.Sprintf("%d%% total (buffers %d%%, summarize %d%%)", u.OverallPercent, u.BufferPercent, u.SummarizePercent)
}

func (u contextUsage) enabled() bool {
	return u.Logs.Cap > 0 || u.Observed.Cap > 0 || u.Chat.Cap > 0 || u.StepWindow.Cap > 0 || u.LogWindow.Cap > 0
}

func (u contextUsage) summarizeRemainingLine() string {
	stepsLeft := "off"
	if rem := u.StepWindow.remaining(); rem >= 0 {
		stepsLeft = fmt.Sprintf("%d", rem)
	}
	logsLeft := "off"
	if rem := u.LogWindow.remaining(); rem >= 0 {
		logsLeft = fmt.Sprintf("%d", rem)
	}
	return fmt.Sprintf("next_summary: steps_left=%s logs_left=%s", stepsLeft, logsLeft)
}

func countNonEmptyFileLines(path string) int {
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	lines := filterNonEmpty(strings.Split(strings.TrimSpace(string(data)), "\n"))
	return len(lines)
}

func (r *Runner) contextUsageSnapshot() (contextUsage, error) {
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		return contextUsage{}, err
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		return contextUsage{}, err
	}
	state, err := memory.LoadState(artifacts.StatePath)
	if err != nil {
		return contextUsage{}, err
	}
	return buildContextUsage(r.cfg, state, countNonEmptyFileLines(artifacts.ChatPath)), nil
}
