package workerloop

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Response struct {
	Type     string `json:"type"`
	Command  string `json:"command,omitempty"`
	UseShell bool   `json:"use_shell,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Question string `json:"question,omitempty"`
}

func ParseResponse(s string) (Response, error) {
	cleaned, err := extractJSON(strings.TrimSpace(s))
	if err != nil {
		return Response{}, err
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cleaned), &envelope); err != nil {
		return Response{}, fmt.Errorf("parse response json: %w", err)
	}

	if nested, ok := envelope["action"]; ok {
		var actionString string
		if err := json.Unmarshal(nested, &actionString); err == nil {
			switch strings.TrimSpace(actionString) {
			case "step_complete":
				summary := ""
				if reason, ok := envelope["reason"]; ok {
					_ = json.Unmarshal(reason, &summary)
				}
				if summary == "" {
					if reasoning, ok := envelope["reasoning"]; ok {
						_ = json.Unmarshal(reasoning, &summary)
					}
				}
				if strings.TrimSpace(summary) == "" {
					summary = "step complete"
				}
				return Response{Type: "step_complete", Summary: summary}, nil
			case "ask_user":
				question := ""
				if rawQuestion, ok := envelope["question"]; ok {
					_ = json.Unmarshal(rawQuestion, &question)
				}
				if strings.TrimSpace(question) == "" {
					if reasoning, ok := envelope["reasoning"]; ok {
						_ = json.Unmarshal(reasoning, &question)
					}
				}
				if strings.TrimSpace(question) == "" {
					return Response{}, fmt.Errorf("ask_user question is required")
				}
				return Response{Type: "ask_user", Question: question}, nil
			}
		}
		if string(nested) == "null" {
			// allow other envelope keys to decide the type
		} else {
			var action struct {
				Command     string `json:"command"`
				UseShell    *bool  `json:"use_shell,omitempty"`
				ShellNeeded *bool  `json:"shell_needed,omitempty"`
			}
			if err := json.Unmarshal(nested, &action); err != nil {
				return Response{}, fmt.Errorf("parse nested action: %w", err)
			}
			r := Response{Type: "action", Command: action.Command}
			if action.UseShell != nil {
				r.UseShell = *action.UseShell
			} else if action.ShellNeeded != nil {
				r.UseShell = *action.ShellNeeded
			}
			if strings.TrimSpace(r.Command) == "" {
				return Response{}, fmt.Errorf("action command is required")
			}
			return r, nil
		}
	}
	if nested, ok := envelope["step_complete"]; ok {
		if string(nested) == "true" {
			summary := ""
			if reason, ok := envelope["reason"]; ok {
				_ = json.Unmarshal(reason, &summary)
			}
			if summary == "" {
				if rawSummary, ok := envelope["summary"]; ok {
					_ = json.Unmarshal(rawSummary, &summary)
				}
			}
			if strings.TrimSpace(summary) == "" {
				summary = "step complete"
			}
			return Response{Type: "step_complete", Summary: summary}, nil
		}
		var complete struct {
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal(nested, &complete); err != nil {
			return Response{}, fmt.Errorf("parse nested step_complete: %w", err)
		}
		if strings.TrimSpace(complete.Summary) == "" {
			return Response{}, fmt.Errorf("step_complete summary is required")
		}
		return Response{Type: "step_complete", Summary: complete.Summary}, nil
	}
	if nested, ok := envelope["ask_user"]; ok {
		if string(nested) == "true" {
			question := ""
			if rawQuestion, ok := envelope["question"]; ok {
				_ = json.Unmarshal(rawQuestion, &question)
			}
			if strings.TrimSpace(question) == "" {
				return Response{}, fmt.Errorf("ask_user question is required")
			}
			return Response{Type: "ask_user", Question: question}, nil
		}
		var ask struct {
			Question string `json:"question"`
		}
		if err := json.Unmarshal(nested, &ask); err != nil {
			return Response{}, fmt.Errorf("parse nested ask_user: %w", err)
		}
		if strings.TrimSpace(ask.Question) == "" {
			return Response{}, fmt.Errorf("ask_user question is required")
		}
		return Response{Type: "ask_user", Question: ask.Question}, nil
	}

	var raw struct {
		Type        string `json:"type"`
		Command     string `json:"command,omitempty"`
		UseShell    *bool  `json:"use_shell,omitempty"`
		ShellNeeded *bool  `json:"shell_needed,omitempty"`
		Summary     string `json:"summary,omitempty"`
		Question    string `json:"question,omitempty"`
	}
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return Response{}, fmt.Errorf("parse response json: %w", err)
	}
	r := Response{
		Type:     raw.Type,
		Command:  raw.Command,
		Summary:  raw.Summary,
		Question: raw.Question,
	}
	if raw.UseShell != nil {
		r.UseShell = *raw.UseShell
	} else if raw.ShellNeeded != nil {
		r.UseShell = *raw.ShellNeeded
	}
	switch r.Type {
	case "action":
		if strings.TrimSpace(r.Command) == "" {
			return Response{}, fmt.Errorf("action command is required")
		}
	case "step_complete":
		if strings.TrimSpace(r.Summary) == "" {
			return Response{}, fmt.Errorf("step_complete summary is required")
		}
	case "ask_user":
		if strings.TrimSpace(r.Question) == "" {
			return Response{}, fmt.Errorf("ask_user question is required")
		}
	default:
		return Response{}, fmt.Errorf("unsupported response type %q", r.Type)
	}
	return r, nil
}

func extractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty response")
	}
	if idx := strings.LastIndex(s, "</think>"); idx >= 0 {
		s = strings.TrimSpace(s[idx+len("</think>"):])
	}
	s = strings.TrimSpace(strings.TrimPrefix(s, "```json"))
	s = strings.TrimSpace(strings.TrimPrefix(s, "```"))
	s = strings.TrimSpace(strings.TrimSuffix(s, "```"))

	candidates := jsonObjectCandidates(s)
	for i := len(candidates) - 1; i >= 0; i-- {
		candidate := candidates[i]
		normalized, probe, ok := parseResponseCandidate(candidate)
		if !ok {
			continue
		}
		if _, ok := probe["type"]; ok {
			return normalized, nil
		}
		if _, ok := probe["action"]; ok {
			return normalized, nil
		}
		if _, ok := probe["step_complete"]; ok {
			return normalized, nil
		}
		if _, ok := probe["ask_user"]; ok {
			return normalized, nil
		}
	}
	return "", fmt.Errorf("no response json object found")
}

func parseResponseCandidate(candidate string) (string, map[string]json.RawMessage, bool) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &probe); err == nil {
		return candidate, probe, true
	}

	normalized := escapeLiteralNewlinesInStrings(candidate)
	if normalized == candidate {
		return "", nil, false
	}
	if err := json.Unmarshal([]byte(normalized), &probe); err != nil {
		return "", nil, false
	}
	return normalized, probe, true
}

func escapeLiteralNewlinesInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	inString := false
	escaped := false
	changed := false

	for _, r := range s {
		if inString {
			if escaped {
				b.WriteRune(r)
				escaped = false
				continue
			}
			switch r {
			case '\\':
				b.WriteRune(r)
				escaped = true
			case '"':
				b.WriteRune(r)
				inString = false
			case '\n':
				b.WriteString(`\n`)
				changed = true
			case '\r':
				changed = true
			default:
				b.WriteRune(r)
			}
			continue
		}

		if r == '"' {
			inString = true
		}
		b.WriteRune(r)
	}

	if !changed {
		return s
	}
	return b.String()
}

func jsonObjectCandidates(s string) []string {
	var out []string
	start := -1
	depth := 0
	inString := false
	escaped := false

	for i, r := range s {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}

		switch r {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, s[start:i+1])
				start = -1
			}
		}
	}
	return out
}
