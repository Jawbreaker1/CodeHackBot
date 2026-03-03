package orchestrator

import "strings"

func sanitizePathComponent(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return "worker"
	}
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

func WorkerSignalID(workerID string) string {
	return "signal-" + workerID
}
