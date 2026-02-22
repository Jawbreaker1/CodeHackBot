package assist

import (
	"os/exec"
	"strings"
)

func normalizeSuggestion(suggestion Suggestion) Suggestion {
	suggestion.Type = strings.ToLower(strings.TrimSpace(suggestion.Type))
	suggestion.Command = strings.TrimSpace(suggestion.Command)
	suggestion.Question = strings.TrimSpace(suggestion.Question)
	suggestion.Summary = strings.TrimSpace(suggestion.Summary)
	suggestion.Final = strings.TrimSpace(suggestion.Final)
	suggestion.Risk = strings.ToLower(strings.TrimSpace(suggestion.Risk))
	suggestion.Plan = strings.TrimSpace(suggestion.Plan)
	suggestion.Steps = normalizeSteps(suggestion.Steps)
	suggestion = normalizeSuggestionType(suggestion)
	// Some models return a full shell invocation in `command` (e.g. `bash -lc '<script>'`).
	// Do a targeted split that preserves the script argument instead of strings.Fields(),
	// which would destroy quoting and frequently yields "unexpected EOF" failures.
	suggestion = splitShellScriptCommandIfNeeded(suggestion)
	if suggestion.Tool != nil {
		suggestion.Tool.Language = strings.ToLower(strings.TrimSpace(suggestion.Tool.Language))
		suggestion.Tool.Name = strings.TrimSpace(suggestion.Tool.Name)
		suggestion.Tool.Purpose = strings.TrimSpace(suggestion.Tool.Purpose)
		suggestion.Tool.Run.Command = strings.TrimSpace(suggestion.Tool.Run.Command)
		for i := range suggestion.Tool.Run.Args {
			suggestion.Tool.Run.Args[i] = strings.TrimSpace(suggestion.Tool.Run.Args[i])
		}
		files := make([]ToolFile, 0, len(suggestion.Tool.Files))
		for _, f := range suggestion.Tool.Files {
			f.Path = strings.TrimSpace(f.Path)
			// Keep content as-is; it may contain leading spaces/newlines.
			if f.Path == "" {
				continue
			}
			files = append(files, f)
		}
		suggestion.Tool.Files = files
	}
	if suggestion.Command != "" && len(suggestion.Args) == 0 {
		parts := strings.Fields(suggestion.Command)
		if len(parts) > 1 {
			suggestion.Command = parts[0]
			suggestion.Args = parts[1:]
		}
	}
	if suggestion.Command != "" && len(suggestion.Args) == 0 {
		suggestion = splitDashCommandIfNeeded(suggestion)
	}
	return suggestion
}

func normalizeSuggestionType(suggestion Suggestion) Suggestion {
	allowed := map[string]struct{}{
		"command": {}, "question": {}, "noop": {}, "plan": {}, "complete": {}, "tool": {},
	}
	if _, ok := allowed[suggestion.Type]; ok {
		return suggestion
	}
	// Compatibility mapping for model variants.
	if suggestion.Type == "recover" {
		switch {
		case suggestion.Tool != nil:
			suggestion.Type = "tool"
		case suggestion.Command != "":
			suggestion.Type = "command"
		case suggestion.Question != "":
			suggestion.Type = "question"
		case len(suggestion.Steps) > 0 || suggestion.Plan != "":
			suggestion.Type = "plan"
		case suggestion.Final != "" || suggestion.Summary != "":
			suggestion.Type = "complete"
		default:
			suggestion.Type = "noop"
		}
		return suggestion
	}
	// Generic fallback mapping for other unknown types.
	switch {
	case suggestion.Tool != nil:
		suggestion.Type = "tool"
	case suggestion.Command != "":
		suggestion.Type = "command"
	case suggestion.Question != "":
		suggestion.Type = "question"
	case len(suggestion.Steps) > 0 || suggestion.Plan != "":
		suggestion.Type = "plan"
	case suggestion.Final != "" || suggestion.Summary != "":
		suggestion.Type = "complete"
	default:
		suggestion.Type = "noop"
	}
	return suggestion
}

func splitShellScriptCommandIfNeeded(suggestion Suggestion) Suggestion {
	if strings.TrimSpace(suggestion.Command) == "" || len(suggestion.Args) != 0 {
		return suggestion
	}
	raw := strings.TrimSpace(suggestion.Command)
	parts := strings.Fields(raw)
	if len(parts) < 3 {
		return suggestion
	}
	shell := strings.ToLower(parts[0])
	if shell != "bash" && shell != "sh" && shell != "zsh" {
		return suggestion
	}
	flag := parts[1]
	if flag != "-c" && flag != "-lc" {
		return suggestion
	}
	prefix := parts[0] + " " + parts[1]
	if !strings.HasPrefix(raw, prefix) {
		return suggestion
	}
	script := strings.TrimSpace(raw[len(prefix):])
	if len(script) >= 2 {
		if (strings.HasPrefix(script, "'") && strings.HasSuffix(script, "'")) ||
			(strings.HasPrefix(script, "\"") && strings.HasSuffix(script, "\"")) {
			script = strings.TrimSpace(script[1 : len(script)-1])
		}
	}
	return Suggestion{
		Type:     suggestion.Type,
		Command:  parts[0],
		Args:     []string{flag, script},
		Question: suggestion.Question,
		Summary:  suggestion.Summary,
		Final:    suggestion.Final,
		Risk:     suggestion.Risk,
		Steps:    suggestion.Steps,
		Plan:     suggestion.Plan,
		Tool:     suggestion.Tool,
	}
}

func normalizeSteps(steps []string) []string {
	if len(steps) == 0 {
		return nil
	}
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		out = append(out, step)
	}
	return out
}

func splitDashCommandIfNeeded(suggestion Suggestion) Suggestion {
	if !strings.Contains(suggestion.Command, "-") {
		return suggestion
	}
	// If full command exists, keep it.
	if _, err := exec.LookPath(suggestion.Command); err == nil {
		return suggestion
	}
	parts := strings.SplitN(suggestion.Command, "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return suggestion
	}
	base := parts[0]
	if _, err := exec.LookPath(base); err != nil {
		return suggestion
	}
	return Suggestion{
		Type:     suggestion.Type,
		Command:  base,
		Args:     []string{"-" + parts[1]},
		Question: suggestion.Question,
		Summary:  suggestion.Summary,
		Risk:     suggestion.Risk,
	}
}
