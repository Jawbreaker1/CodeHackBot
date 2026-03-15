package msf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AdaptRuntimeCommand applies metasploit-specific runtime fixes so command
// execution is stable across CLI /assist, tool-forge, and orchestrator workers.
func AdaptRuntimeCommand(command string, args []string, workingDir string) (string, []string, []string, error) {
	command = strings.TrimSpace(command)
	args = compactArgs(args)
	if command == "" {
		return command, args, nil, nil
	}
	notes := make([]string, 0, 3)

	if isMSFConsole(command) {
		if next, changed := rewriteMSFConsoleArgs(args); changed {
			args = next
			notes = append(notes, "Runtime adaptation: normalized msfconsole execute flag to -x")
		}
		return command, args, notes, nil
	}

	if isShellCommand(command) && len(args) >= 2 && (args[0] == "-c" || args[0] == "-lc") {
		if body, changed := rewriteMSFConsoleExecuteFlag(args[1]); changed {
			args[1] = body
			notes = append(notes, "Runtime adaptation: normalized msfconsole execute flag in shell expression")
		}
	}

	if path, patched, err := maybePatchMSFConsoleScript(command, args, workingDir); err != nil {
		return command, args, notes, err
	} else if patched {
		notes = append(notes, fmt.Sprintf("Runtime adaptation: patched msfconsole -e to -x in %s", path))
	}

	if adaptedCmd, adaptedArgs, note, ok, err := maybeAdaptRubyMSFScript(command, args, workingDir); err != nil {
		return command, args, notes, err
	} else if ok {
		command = adaptedCmd
		args = adaptedArgs
		notes = append(notes, note)
	}

	return command, args, notes, nil
}

func compactArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func isMSFConsole(command string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	return base == "msfconsole"
}

func isShellCommand(command string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	return base == "bash" || base == "sh" || base == "zsh"
}

func rewriteMSFConsoleArgs(args []string) ([]string, bool) {
	if len(args) == 0 {
		return args, false
	}
	out := make([]string, 0, len(args))
	changed := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		switch {
		case lower == "-e":
			out = append(out, "-x")
			changed = true
		case strings.HasPrefix(lower, "-e") && len(arg) > 2:
			out = append(out, "-x"+arg[2:])
			changed = true
		case lower == "--execute-command":
			out = append(out, "-x")
			changed = true
		case strings.HasPrefix(lower, "--execute-command="):
			value := strings.TrimSpace(strings.SplitN(arg, "=", 2)[1])
			out = append(out, "-x")
			if value != "" {
				out = append(out, value)
			}
			changed = true
		default:
			out = append(out, arg)
		}
	}
	if !changed {
		return args, false
	}
	return out, true
}

func rewriteMSFConsoleExecuteFlag(content string) (string, bool) {
	if strings.TrimSpace(content) == "" {
		return content, false
	}
	lines := strings.Split(content, "\n")
	changed := false
	for i, line := range lines {
		if !strings.Contains(line, "msfconsole") {
			continue
		}
		updated := line
		updated = strings.Replace(updated, " -e ", " -x ", 1)
		updated = strings.Replace(updated, " -e\"", " -x\"", 1)
		updated = strings.Replace(updated, " -e'", " -x'", 1)
		if updated != line {
			lines[i] = updated
			changed = true
		}
	}
	if !changed {
		return content, false
	}
	return strings.Join(lines, "\n"), true
}

func maybePatchMSFConsoleScript(command string, args []string, workingDir string) (string, bool, error) {
	if !isShellCommand(command) {
		return "", false, nil
	}
	if len(args) == 0 {
		return "", false, nil
	}
	if args[0] == "-c" || args[0] == "-lc" {
		return "", false, nil
	}
	scriptPath := firstNonFlagArg(args)
	if scriptPath == "" {
		return "", false, nil
	}
	resolved := resolveRuntimePath(scriptPath, workingDir)
	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return resolved, false, err
	}
	patched, changed := rewriteMSFConsoleExecuteFlag(string(data))
	if !changed {
		return resolved, false, nil
	}
	if err := os.WriteFile(resolved, []byte(patched), 0o644); err != nil {
		return resolved, false, err
	}
	return resolved, true, nil
}

func maybeAdaptRubyMSFScript(command string, args []string, workingDir string) (string, []string, string, bool, error) {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	if !strings.HasPrefix(base, "ruby") {
		return command, args, "", false, nil
	}
	scriptPath := firstNonFlagArg(args)
	if scriptPath == "" {
		return command, args, "", false, nil
	}
	resolved := resolveRuntimePath(scriptPath, workingDir)
	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return command, args, "", false, nil
		}
		return command, args, "", false, err
	}
	if !looksLikeMetasploitRubyScript(string(data)) {
		return command, args, "", false, nil
	}
	expr := "if [ -f /usr/share/metasploit-framework/msfenv ]; then . /usr/share/metasploit-framework/msfenv; fi; ruby " + shellSingleQuote(resolved)
	return "bash", []string{"-lc", expr}, "Runtime adaptation: executing metasploit ruby script via msfenv bootstrap", true, nil
}

func looksLikeMetasploitRubyScript(content string) bool {
	lower := strings.ToLower(content)
	needles := []string{
		"require 'msf/",
		"require \"msf/",
		"require 'rex/",
		"require \"rex/",
		"/usr/share/metasploit-framework/lib/msf",
	}
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func firstNonFlagArg(args []string) string {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" || strings.HasPrefix(trimmed, "-") {
			continue
		}
		return trimmed
	}
	return ""
}

func resolveRuntimePath(pathArg string, workingDir string) string {
	trimmed := strings.TrimSpace(pathArg)
	if trimmed == "" {
		return trimmed
	}
	if filepath.IsAbs(trimmed) {
		return filepath.Clean(trimmed)
	}
	if strings.TrimSpace(workingDir) != "" {
		return filepath.Clean(filepath.Join(workingDir, trimmed))
	}
	return filepath.Clean(trimmed)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
