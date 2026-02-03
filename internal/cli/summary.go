package cli

import (
	"fmt"
	"strings"
	"time"
)

const (
	ansiReset  = "\x1b[0m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
)

const outputPreviewLines = 5

func formatStatus(ok bool) string {
	if ok {
		return ansiGreen + "ok" + ansiReset
	}
	return ansiRed + "error" + ansiReset
}

func renderExecSummary(command string, args []string, duration time.Duration, logPath, ledgerStatus, output string, execErr error) string {
	cmd := strings.Join(append([]string{command}, args...), " ")
	status := formatStatus(execErr == nil)
	if ledgerStatus == "" {
		ledgerStatus = "disabled"
	}
	var b strings.Builder
	b.WriteString("--- Execution Summary ---\n")
	b.WriteString(fmt.Sprintf("Status: %s (%s)\n", status, duration.Round(time.Millisecond)))
	b.WriteString(fmt.Sprintf("Command: %s\n", cmd))
	if logPath != "" {
		b.WriteString(fmt.Sprintf("Log: %s\n", logPath))
	}
	b.WriteString(fmt.Sprintf("Ledger: %s\n", ledgerStatus))
	if execErr != nil {
		b.WriteString(fmt.Sprintf("Error: %s%s%s\n", ansiYellow, execErr.Error(), ansiReset))
	}
	preview := previewOutput(output, outputPreviewLines)
	b.WriteString("Output (first 5 lines):\n")
	if preview == "" {
		b.WriteString("(no output)\n")
	} else {
		b.WriteString(preview)
		if !strings.HasSuffix(preview, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderInitSummary(sessionDir, configPath string, inventoryCaptured bool) string {
	status := formatStatus(true)
	inventory := "skipped"
	if inventoryCaptured {
		inventory = "captured"
	}
	return fmt.Sprintf("--- Init Summary ---\nStatus: %s\nSession: %s\nConfig: %s\nInventory: %s\n", status, sessionDir, configPath, inventory)
}

func previewOutput(output string, maxLines int) string {
	if output == "" || maxLines <= 0 {
		return ""
	}
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	preview := append(lines[:maxLines], "...")
	return strings.Join(preview, "\n")
}
