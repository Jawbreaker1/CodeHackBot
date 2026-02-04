package llm

import "time"

type Guard struct {
	maxFailures   int
	cooldown      time.Duration
	failures      int
	disabledUntil time.Time
	now           func() time.Time
}

func NewGuard(maxFailures int, cooldown time.Duration) Guard {
	return Guard{
		maxFailures: maxFailures,
		cooldown:    cooldown,
		now:         time.Now,
	}
}

func (g *Guard) Allow() bool {
	if g == nil {
		return true
	}
	if g.disabledUntil.IsZero() {
		return true
	}
	return g.now().After(g.disabledUntil)
}

func (g *Guard) RecordFailure() {
	if g == nil || g.maxFailures <= 0 {
		return
	}
	g.failures++
	if g.failures >= g.maxFailures {
		g.disabledUntil = g.now().Add(g.cooldown)
	}
}

func (g *Guard) RecordSuccess() {
	if g == nil {
		return
	}
	g.failures = 0
	g.disabledUntil = time.Time{}
}

func (g *Guard) DisabledUntil() time.Time {
	if g == nil {
		return time.Time{}
	}
	return g.disabledUntil
}

func (g *Guard) Failures() int {
	if g == nil {
		return 0
	}
	return g.failures
}
