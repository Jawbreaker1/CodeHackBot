package orchestrator

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
)

const (
	workerAssistMaxSuggestionSteps = 16
	workerAssistMaxStepLength      = 400
	workerAssistMaxToolRunArgs     = 64
	workerAssistMaxToolArgLength   = 2048
)

func validateAssistSuggestionSchema(suggestion assist.Suggestion) error {
	if len(suggestion.Steps) > workerAssistMaxSuggestionSteps {
		return fmt.Errorf("assistant suggestion steps exceed max (%d > %d)", len(suggestion.Steps), workerAssistMaxSuggestionSteps)
	}
	for i, step := range suggestion.Steps {
		step = strings.TrimSpace(step)
		if step == "" {
			return fmt.Errorf("assistant suggestion steps[%d] is empty", i)
		}
		if len(step) > workerAssistMaxStepLength {
			return fmt.Errorf("assistant suggestion steps[%d] exceeds max length", i)
		}
	}

	if suggestion.Tool == nil {
		if strings.EqualFold(strings.TrimSpace(suggestion.Type), "tool") {
			return fmt.Errorf("assistant tool suggestion missing spec")
		}
		return nil
	}
	if len(suggestion.Tool.Files) == 0 {
		return fmt.Errorf("assistant tool suggestion has no files")
	}
	if len(suggestion.Tool.Files) > 20 {
		return fmt.Errorf("assistant tool suggestion too many files (%d > 20)", len(suggestion.Tool.Files))
	}
	for i, file := range suggestion.Tool.Files {
		toolPath := strings.TrimSpace(file.Path)
		if toolPath == "" {
			return fmt.Errorf("assistant tool suggestion files[%d] missing path", i)
		}
		if filepath.IsAbs(toolPath) {
			return fmt.Errorf("assistant tool suggestion files[%d] path must be relative", i)
		}
		clean := filepath.Clean(toolPath)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("assistant tool suggestion files[%d] path escapes tools root", i)
		}
		if len(file.Content) > workerAssistWriteMaxBytes {
			return fmt.Errorf("assistant tool suggestion files[%d] content exceeds max size", i)
		}
	}
	runCommand := strings.TrimSpace(suggestion.Tool.Run.Command)
	if runCommand == "" {
		return fmt.Errorf("assistant tool suggestion missing run.command")
	}
	if len(suggestion.Tool.Run.Args) > workerAssistMaxToolRunArgs {
		return fmt.Errorf("assistant tool suggestion run.args exceed max (%d > %d)", len(suggestion.Tool.Run.Args), workerAssistMaxToolRunArgs)
	}
	for i, arg := range suggestion.Tool.Run.Args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			return fmt.Errorf("assistant tool suggestion run.args[%d] is empty", i)
		}
		if len(trimmed) > workerAssistMaxToolArgLength {
			return fmt.Errorf("assistant tool suggestion run.args[%d] exceeds max length", i)
		}
	}
	return nil
}
