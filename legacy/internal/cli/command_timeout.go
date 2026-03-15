package cli

import (
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultIdleTimeout  = 30 * time.Second
	longScanIdleTimeout = 20 * time.Minute
	msfIdleTimeout      = 10 * time.Minute
)

func resolveShellIdleTimeout(base time.Duration, command string, args []string) time.Duration {
	timeout := base
	if timeout <= 0 {
		timeout = defaultIdleTimeout
	}
	switch classifyCommand(command, args) {
	case "nmap":
		if timeout < longScanIdleTimeout {
			timeout = longScanIdleTimeout
		}
	case "msfconsole":
		if timeout < msfIdleTimeout {
			timeout = msfIdleTimeout
		}
	}
	return timeout
}

func classifyCommand(command string, args []string) string {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "nmap":
		return "nmap"
	case "msfconsole":
		return "msfconsole"
	case "bash", "sh", "zsh":
		if len(args) >= 2 {
			flag := strings.TrimSpace(args[0])
			body := strings.ToLower(strings.TrimSpace(args[1]))
			if (flag == "-c" || flag == "-lc") && strings.Contains(body, "nmap") {
				return "nmap"
			}
			if (flag == "-c" || flag == "-lc") && strings.Contains(body, "msfconsole") {
				return "msfconsole"
			}
		}
	}
	return ""
}
