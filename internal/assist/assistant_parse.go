package assist

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func (e SuggestionParseError) Error() string {
	if e.Err == nil {
		return "parse suggestion json"
	}
	return fmt.Sprintf("parse suggestion json: %v", e.Err)
}

func (e SuggestionParseError) Unwrap() error {
	return e.Err
}

func selectFloat32(value *float32, fallback float32) float32 {
	if value == nil {
		return fallback
	}
	return *value
}

func selectInt(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func parseSuggestionResponse(content string) (Suggestion, *SuggestionParseError) {
	raw := extractJSON(content)
	var suggestion Suggestion
	if err := json.Unmarshal([]byte(raw), &suggestion); err != nil {
		if loose, looseErr := parseSuggestionLoose(raw); looseErr == nil {
			return normalizeSuggestion(loose), nil
		}
		fallback := parseSimpleCommand(content)
		if fallback != "" {
			return normalizeSuggestion(Suggestion{
				Type:    "command",
				Command: fallback,
				Summary: "Recovered command from plain-text response.",
				Risk:    "low",
			}), nil
		}
		parseErr := SuggestionParseError{Raw: content, Err: err}
		return Suggestion{}, &parseErr
	}
	return normalizeSuggestion(suggestion), nil
}

func parseSuggestionLoose(raw string) (Suggestion, error) {
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return Suggestion{}, err
	}
	out := Suggestion{
		Type:     coerceString(payload["type"]),
		Command:  coerceString(payload["command"]),
		Args:     coerceStringSlice(payload["args"]),
		Question: coerceString(payload["question"]),
		Summary:  coerceString(payload["summary"]),
		Final:    coerceString(payload["final"]),
		Risk:     coerceString(payload["risk"]),
		Steps:    coerceSteps(payload["steps"]),
		Plan:     coerceString(payload["plan"]),
	}
	if tool, ok := coerceToolSpec(payload["tool"]); ok {
		out.Tool = tool
	}
	return out, nil
}

func coerceString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64, bool, int, int64, uint64:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	default:
		return ""
	}
}

func coerceStringSlice(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return nil
		}
		if strings.ContainsAny(trimmed, " \t\n") {
			return strings.Fields(trimmed)
		}
		return []string{trimmed}
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := coerceString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func coerceSteps(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := coerceString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return coerceStringSlice(t)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for key := range t {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([]string, 0, len(keys))
		for _, key := range keys {
			if s := coerceString(t[key]); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func coerceToolSpec(v any) (*ToolSpec, bool) {
	toolMap, ok := v.(map[string]any)
	if !ok || len(toolMap) == 0 {
		return nil, false
	}
	tool := &ToolSpec{
		Language: coerceString(toolMap["language"]),
		Name:     coerceString(toolMap["name"]),
		Purpose:  coerceString(toolMap["purpose"]),
	}
	if files, ok := toolMap["files"].([]any); ok {
		tool.Files = make([]ToolFile, 0, len(files))
		for _, fileAny := range files {
			fileMap, ok := fileAny.(map[string]any)
			if !ok {
				continue
			}
			path := coerceString(fileMap["path"])
			if path == "" {
				continue
			}
			tool.Files = append(tool.Files, ToolFile{
				Path:    path,
				Content: coerceString(fileMap["content"]),
			})
		}
	}
	if runMap, ok := toolMap["run"].(map[string]any); ok {
		tool.Run = ToolRun{
			Command: coerceString(runMap["command"]),
			Args:    coerceStringSlice(runMap["args"]),
		}
	}
	return tool, true
}
