package msf

import (
	"fmt"
	"strings"
)

type Query struct {
	Service  string
	Keyword  string
	Platform string
}

func BuildSearch(q Query) string {
	parts := []string{}
	if q.Service != "" {
		parts = append(parts, fmt.Sprintf("service:%s", q.Service))
	}
	if q.Platform != "" {
		parts = append(parts, fmt.Sprintf("platform:%s", q.Platform))
	}
	if q.Keyword != "" {
		parts = append(parts, q.Keyword)
	}
	return strings.Join(parts, " ")
}

func BuildCommand(search string) string {
	if strings.TrimSpace(search) == "" {
		return "search; exit"
	}
	return fmt.Sprintf("search %s; exit", search)
}

func ParseSearchOutput(output string) []string {
	lines := []string{}
	for _, line := range strings.Split(output, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "Matching Modules") || strings.HasPrefix(trim, "======") {
			continue
		}
		if strings.HasPrefix(trim, "No results from search") {
			continue
		}
		lower := strings.ToLower(trim)
		if strings.HasPrefix(lower, "exploit/") || strings.HasPrefix(lower, "auxiliary/") || strings.HasPrefix(lower, "post/") || strings.HasPrefix(lower, "payload/") {
			lines = append(lines, trim)
		}
	}
	return lines
}
