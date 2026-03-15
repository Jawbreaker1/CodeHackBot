package session

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type InventoryCommand struct {
	Label   string
	Command string
	Args    []string
}

func DefaultInventoryCommands() []InventoryCommand {
	return []InventoryCommand{
		{Label: "OS / Kernel", Command: "uname", Args: []string{"-a"}},
		{Label: "User", Command: "whoami"},
		{Label: "Privilege", Command: "id", Args: []string{"-u"}},
		{Label: "Shell", Command: "bash", Args: []string{"--version"}},
		{Label: "Python", Command: "python3", Args: []string{"--version"}},
		{Label: "Python (fallback)", Command: "python", Args: []string{"--version"}},
		{Label: "nmap", Command: "nmap", Args: []string{"--version"}},
		{Label: "Metasploit", Command: "msfconsole", Args: []string{"--version"}},
		{Label: "Disk", Command: "df", Args: []string{"-h", "."}},
	}
}

func WriteInventory(sessionDir, filename string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	path := filepath.Join(sessionDir, filename)
	var out bytes.Buffer
	out.WriteString("# Inventory\n\n")
	out.WriteString(fmt.Sprintf("Collected (UTC): %s\n\n", time.Now().UTC().Format(time.RFC3339)))

	for _, cmd := range DefaultInventoryCommands() {
		out.WriteString(fmt.Sprintf("## %s\n\n", cmd.Label))
		result := runCommand(cmd.Command, cmd.Args, timeout)
		out.WriteString("```\n")
		out.WriteString(result)
		if !strings.HasSuffix(result, "\n") {
			out.WriteString("\n")
		}
		out.WriteString("```\n\n")
	}

	return os.WriteFile(path, out.Bytes(), 0o644)
}

func runCommand(command string, args []string, timeout time.Duration) string {
	if _, err := exec.LookPath(command); err != nil {
		return fmt.Sprintf("not found: %s", command)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("timeout after %s", timeout)
	}
	if err != nil {
		return fmt.Sprintf("error: %v\n%s", err, string(output))
	}
	return strings.TrimSpace(string(output))
}
