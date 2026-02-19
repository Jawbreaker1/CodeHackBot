package orchestrator

import (
	"fmt"
	"sort"
	"strings"
)

const maxDefaultHypotheses = 5

type hypothesisTemplate struct {
	keywords   []string
	statement  string
	confidence string
	impact     string
	baseScore  int
	success    []string
	fail       []string
	evidence   []string
}

var defaultHypothesisTemplates = []hypothesisTemplate{
	{
		keywords:   []string{"web", "http", "https", "site", "application", "api"},
		statement:  "Web-facing attack surface may expose misconfiguration or weak input handling.",
		confidence: "medium",
		impact:     "medium",
		baseScore:  70,
		success:    []string{"endpoints mapped", "headers captured", "input vectors identified"},
		fail:       []string{"no reachable web surface", "scope has no web targets"},
		evidence:   []string{"endpoint inventory", "http headers", "response samples"},
	},
	{
		keywords:   []string{"network", "lan", "subnet", "host", "port", "service", "scan"},
		statement:  "Network services may expose unnecessary or misconfigured ports.",
		confidence: "high",
		impact:     "high",
		baseScore:  80,
		success:    []string{"open ports enumerated", "service versions fingerprinted"},
		fail:       []string{"host unreachable", "all probes blocked"},
		evidence:   []string{"port scan output", "service banner/version evidence"},
	},
	{
		keywords:   []string{"auth", "login", "token", "session", "credential", "password"},
		statement:  "Authentication or session controls may allow weak or bypassable access.",
		confidence: "medium",
		impact:     "high",
		baseScore:  75,
		success:    []string{"auth paths mapped", "session controls validated"},
		fail:       []string{"no auth surface discovered"},
		evidence:   []string{"auth flow notes", "session cookie/header analysis"},
	},
	{
		keywords:   []string{"privilege", "escalation", "sudo", "root", "admin"},
		statement:  "Privilege boundaries may be bypassed under reachable conditions.",
		confidence: "medium",
		impact:     "high",
		baseScore:  72,
		success:    []string{"privileged path identified", "boundary checks validated"},
		fail:       []string{"no privilege boundary reachable"},
		evidence:   []string{"permission model notes", "escalation attempt outcomes"},
	},
	{
		keywords:   []string{"zip", "archive", "hash", "john", "fcrackzip", "crack"},
		statement:  "Archive protection may be weak against available cracking strategies.",
		confidence: "medium",
		impact:     "medium",
		baseScore:  68,
		success:    []string{"archive metadata extracted", "crack strategy validated"},
		fail:       []string{"archive not found", "no viable cracking method"},
		evidence:   []string{"archive metadata", "hash material", "strategy run logs"},
	},
	{
		keywords:   []string{},
		statement:  "Initial recon may reveal exposed assets requiring deeper validation.",
		confidence: "low",
		impact:     "medium",
		baseScore:  55,
		success:    []string{"scope inventory complete", "candidate findings triaged"},
		fail:       []string{"insufficient reconnaissance data"},
		evidence:   []string{"scope inventory", "tool output logs"},
	},
}

func GenerateHypotheses(goal string, scope Scope, maxCount int) []Hypothesis {
	normalizedGoal := strings.ToLower(strings.TrimSpace(goal))
	if maxCount <= 0 {
		maxCount = maxDefaultHypotheses
	}
	targets := append(append([]string{}, scope.Targets...), scope.Networks...)
	if len(targets) == 0 {
		targets = []string{"in-scope targets"}
	}

	candidates := make([]Hypothesis, 0, len(defaultHypothesisTemplates))
	for idx, tpl := range defaultHypothesisTemplates {
		matches := countKeywordMatches(normalizedGoal, tpl.keywords)
		if len(tpl.keywords) > 0 && matches == 0 {
			continue
		}
		targetHint := targets[0]
		statement := tpl.statement
		if targetHint != "" {
			statement = fmt.Sprintf("%s Target: %s.", strings.TrimSuffix(statement, "."), targetHint)
		}
		score := tpl.baseScore + (matches * 5)
		candidates = append(candidates, Hypothesis{
			ID:               fmt.Sprintf("H-%02d", idx+1),
			Statement:        statement,
			Confidence:       tpl.confidence,
			Impact:           tpl.impact,
			Score:            score,
			SuccessSignals:   append([]string{}, tpl.success...),
			FailSignals:      append([]string{}, tpl.fail...),
			EvidenceRequired: append([]string{}, tpl.evidence...),
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Impact != candidates[j].Impact {
			return impactRank(candidates[i].Impact) > impactRank(candidates[j].Impact)
		}
		return candidates[i].ID < candidates[j].ID
	})

	if len(candidates) > maxCount {
		candidates = candidates[:maxCount]
	}
	return candidates
}

func countKeywordMatches(goal string, keywords []string) int {
	if goal == "" || len(keywords) == 0 {
		return 0
	}
	matchCount := 0
	for _, keyword := range keywords {
		if strings.Contains(goal, keyword) {
			matchCount++
		}
	}
	return matchCount
}

func impactRank(impact string) int {
	switch strings.ToLower(strings.TrimSpace(impact)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
