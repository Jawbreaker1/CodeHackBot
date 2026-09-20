package execx

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const outputPreviewBytes = 8 * 1024

func readOutputSummary(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, outputPreviewBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > outputPreviewBytes {
		return strings.TrimSpace(string(data[:outputPreviewBytes])) + "\n[preview truncated; read the output artifact for full evidence]", nil
	}
	if text := strings.TrimSpace(string(data)); text != "" {
		return text, nil
	}
	return "(none)", nil
}

func writeLogStart(path string, plan Plan, started time.Time) error {
	content := strings.Join([]string{
		"action: " + plan.Requested,
		"actual_invocation: " + plan.ActualExec,
		"execution_mode: " + plan.ExecutionMode,
		"cwd: " + blankOrNone(plan.Action.Cwd),
		"started_at: " + started.Format(time.RFC3339Nano),
		"stdout_path: " + path + ".stdout",
		"stderr_path: " + path + ".stderr",
		"status: running", "",
	}, "\n")
	return os.WriteFile(path, []byte(content), 0o600)
}

func writeLogFinish(path string, finished time.Time, exitStatus int, failureClass string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	status := "completed"
	if failureClass != "" {
		status = "failed"
	}
	if failureClass == "execution_interrupted" {
		status = "aborted"
	}
	if _, err := fmt.Fprintf(file, "finished_at: %s\nexit_status: %d\nstatus: %s\n", finished.Format(time.RFC3339Nano), exitStatus, status); err != nil {
		return err
	}
	for _, stream := range []string{"stdout", "stderr"} {
		if _, err := fmt.Fprintf(file, "\n[%s]\n", stream); err != nil {
			return err
		}
		input, err := os.Open(path + "." + stream)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return file.Close()
}
