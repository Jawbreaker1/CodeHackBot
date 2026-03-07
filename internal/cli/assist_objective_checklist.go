package cli

import "strings"

func objectiveChecklist(goal string) []string {
	lower := strings.ToLower(strings.TrimSpace(goal))
	if lower == "" || !looksLikeAction(lower) {
		return nil
	}
	items := make([]string, 0, 4)
	add := func(item string) {
		item = strings.TrimSpace(item)
		if item == "" {
			return
		}
		for _, existing := range items {
			if strings.EqualFold(existing, item) {
				return
			}
		}
		items = append(items, item)
	}

	if strings.Contains(lower, "password") || strings.Contains(lower, "passphrase") || strings.Contains(lower, "decrypt") || strings.Contains(lower, "crack") {
		add("Prove credential/key recovery with concrete evidence output (not just tool completion text).")
	}
	if strings.Contains(lower, "content") || strings.Contains(lower, "contents") || strings.Contains(lower, "extract") || strings.Contains(lower, "inside") {
		add("Show extracted artifact contents or an explicit listing from a successful extraction/read step.")
	}
	if strings.Contains(lower, "report") {
		add("Produce the requested report artifact and summarize key findings in plain language.")
	}
	if strings.Contains(lower, "vulnerab") || strings.Contains(lower, "cve") || strings.Contains(lower, "exploit") || strings.Contains(lower, "misconfig") {
		add("For each vulnerability/CVE claim, include source-backed evidence refs (tool/advisory output) and target-applicability validation evidence before marking complete.")
	}
	if strings.Contains(lower, "scan") || strings.Contains(lower, "identify") || strings.Contains(lower, "find") || strings.Contains(lower, "verify") {
		add("Tie conclusions to concrete evidence refs (logs/artifacts) before marking objective complete.")
	}
	if len(items) == 0 {
		add("Do not mark complete until objective claims are directly supported by evidence refs.")
	}
	return items
}
