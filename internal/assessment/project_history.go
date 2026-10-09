package assessment

import (
	"sort"
	"time"
)

// ProjectHistory is a bounded snapshot of earlier sessions on the same
// explicitly identified target. It is context, not evidence for this run.
type ProjectHistory struct {
	Project         string         `json:"project"`
	Target          string         `json:"target"`
	Sessions        []PriorSession `json:"sessions"`
	OmittedSessions int            `json:"omitted_sessions,omitempty"`
}

type PriorSession struct {
	ID              string         `json:"id"`
	Goal            string         `json:"goal"`
	Status          string         `json:"status"`
	FinishedAt      time.Time      `json:"finished_at,omitempty"`
	EvidenceIndex   string         `json:"evidence_index,omitempty"`
	Findings        []PriorFinding `json:"findings,omitempty"`
	OmittedFindings int            `json:"omitted_findings,omitempty"`
	Reviews         []PriorReview  `json:"reviews,omitempty"`
	Gaps            []string       `json:"gaps,omitempty"`
	OmittedGaps     int            `json:"omitted_gaps,omitempty"`
}

type PriorFinding struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Severity    string `json:"severity,omitempty"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

// PriorReview records what a new worker actually established about an older
// finding. A missing finding in a later scan is never treated as a resolution.
type PriorReview struct {
	FindingID      string   `json:"finding_id"`
	Verdict        string   `json:"verdict"` // still_present, not_observed, remediated, inconclusive
	Reason         string   `json:"reason"`
	ValidationTask string   `json:"validation_task"`
	Evidence       []string `json:"evidence"`
}

// CurrentPriorReviews returns the latest recorded verdict per earlier finding.
func CurrentPriorReviews(plans []Decision) []PriorReview {
	latest := map[string]PriorReview{}
	for _, plan := range plans {
		for _, review := range plan.PriorReviews {
			latest[review.FindingID] = review
		}
	}
	ids := make([]string, 0, len(latest))
	for id := range latest {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	reviews := make([]PriorReview, 0, len(ids))
	for _, id := range ids {
		reviews = append(reviews, latest[id])
	}
	return reviews
}
