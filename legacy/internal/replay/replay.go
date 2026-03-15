package replay

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/exec"
)

type Step struct {
	Command string
	Args    []string
	Raw     string
}

func FilePath(baseDir, sessionID string) string {
	return filepath.Join(baseDir, sessionID, "replay.txt")
}

func LoadSteps(path string) ([]Step, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open replay file: %w", err)
	}
	defer file.Close()

	steps := []Step{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		steps = append(steps, Step{
			Command: parts[0],
			Args:    parts[1:],
			Raw:     line,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan replay file: %w", err)
	}
	return steps, nil
}

func RunSteps(runner exec.Runner, steps []Step) ([]exec.CommandResult, error) {
	results := make([]exec.CommandResult, 0, len(steps))
	for _, step := range steps {
		result, err := runner.RunCommand(step.Command, step.Args...)
		results = append(results, result)
		if err != nil {
			return results, err
		}
	}
	return results, nil
}
