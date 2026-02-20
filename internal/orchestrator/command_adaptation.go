package orchestrator

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
)

var currentUserLookup = user.Current

func adaptCommandForRuntime(scopePolicy *ScopePolicy, command string, args []string) (string, []string, string, bool) {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	if !strings.EqualFold(command, "nmap") {
		return command, args, "", false
	}
	adapted := false
	notes := make([]string, 0, 3)

	if nextArgs, note, ok := removeMissingNmapInputList(args); ok {
		args = nextArgs
		adapted = true
		if note != "" {
			notes = append(notes, note)
		}
	}
	if nextArgs, note, ok := downgradeSynScanForUnprivilegedUser(args); ok {
		args = nextArgs
		adapted = true
		if note != "" {
			notes = append(notes, note)
		}
	}
	if scopePolicy != nil && !hasNmapFlag(args, "-sn") && hasNmapFingerprintArgs(args) {
		target, ok := firstBroadCIDRTarget(scopePolicy.extractTargets(command, args))
		if ok {
			nextArgs := buildNmapDiscoveryArgs(args, target)
			if !stringSlicesEqual(args, nextArgs) {
				args = nextArgs
				adapted = true
				notes = append(notes, fmt.Sprintf("adapted nmap for broad target %s: switched to host discovery (-sn) to avoid timeout; run targeted service scans on discovered hosts next", target))
			}
		}
	}
	return command, args, strings.Join(notes, "; "), adapted
}

func hasNmapFingerprintArgs(args []string) bool {
	for _, raw := range args {
		arg := strings.ToLower(strings.TrimSpace(raw))
		switch arg {
		case "-sv", "-o", "-a", "--version-all", "--osscan-guess":
			return true
		}
	}
	return false
}

func hasNmapFlag(args []string, flag string) bool {
	flag = strings.ToLower(strings.TrimSpace(flag))
	for _, raw := range args {
		if strings.ToLower(strings.TrimSpace(raw)) == flag {
			return true
		}
	}
	return false
}

func firstBroadCIDRTarget(targets []string) (string, bool) {
	for _, target := range targets {
		trimmed := strings.TrimSpace(target)
		if trimmed == "" || !strings.Contains(trimmed, "/") {
			continue
		}
		_, network, err := net.ParseCIDR(trimmed)
		if err != nil || network == nil {
			continue
		}
		ones, bits := network.Mask.Size()
		if bits == 32 && ones <= 24 {
			return trimmed, true
		}
		if bits == 128 && ones <= 120 {
			return trimmed, true
		}
	}
	return "", false
}

func buildNmapDiscoveryArgs(args []string, target string) []string {
	out := []string{"-sn"}
	if hasNmapFlag(args, "-n") {
		out = append(out, "-n")
	}
	if hasNmapVerbosity(args) {
		out = append(out, "-v")
	}
	if !hasNmapOption(args, "--host-timeout") {
		out = append(out, "--host-timeout", "10s")
	}
	if !hasNmapOption(args, "--max-retries") {
		out = append(out, "--max-retries", "1")
	}
	out = append(out, nmapOutputArgs(args)...)
	out = append(out, target)
	return out
}

func hasNmapVerbosity(args []string) bool {
	for _, raw := range args {
		arg := strings.ToLower(strings.TrimSpace(raw))
		if arg == "-v" || arg == "-vv" || arg == "-vvv" {
			return true
		}
	}
	return false
}

func hasNmapOption(args []string, option string) bool {
	option = strings.ToLower(strings.TrimSpace(option))
	for _, raw := range args {
		arg := strings.ToLower(strings.TrimSpace(raw))
		if arg == option || strings.HasPrefix(arg, option+"=") {
			return true
		}
	}
	return false
}

func nmapOutputArgs(args []string) []string {
	out := make([]string, 0, 4)
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		switch lower {
		case "-on", "-og", "-ox", "-oa":
			out = append(out, arg)
			if i+1 < len(args) {
				out = append(out, strings.TrimSpace(args[i+1]))
				i++
			}
		default:
			if strings.HasPrefix(lower, "-on") || strings.HasPrefix(lower, "-og") || strings.HasPrefix(lower, "-ox") || strings.HasPrefix(lower, "-oa") {
				out = append(out, arg)
			}
		}
	}
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func removeMissingNmapInputList(args []string) ([]string, string, bool) {
	if len(args) == 0 {
		return args, "", false
	}
	out := make([]string, 0, len(args))
	removed := []string{}
	changed := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		if lower == "-il" {
			if i+1 < len(args) {
				path := strings.TrimSpace(args[i+1])
				if shouldDropNmapInputList(path) {
					removed = append(removed, path)
					changed = true
					i++
					continue
				}
			}
			out = append(out, arg)
			continue
		}
		if strings.HasPrefix(lower, "-il") && len(arg) > 3 {
			path := strings.TrimSpace(arg[3:])
			if shouldDropNmapInputList(path) {
				removed = append(removed, path)
				changed = true
				continue
			}
		}
		out = append(out, arg)
	}
	if !changed {
		return args, "", false
	}
	note := "removed missing nmap -iL file"
	if len(removed) == 1 {
		note += ": " + removed[0]
	} else if len(removed) > 1 {
		note += "s: " + strings.Join(removed, ", ")
	}
	return out, note, true
}

func shouldDropNmapInputList(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return true
	}
	if !strings.HasPrefix(path, "/") {
		// Relative paths may resolve in worker-specific directories; keep them.
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return false
	}
	return true
}

func downgradeSynScanForUnprivilegedUser(args []string) ([]string, string, bool) {
	if len(args) == 0 || runningAsRoot() {
		return args, "", false
	}
	changed := false
	out := append([]string{}, args...)
	for i, raw := range out {
		if strings.EqualFold(strings.TrimSpace(raw), "-sS") {
			out[i] = "-sT"
			changed = true
		}
	}
	if !changed {
		return args, "", false
	}
	return out, "downgraded nmap SYN scan (-sS) to connect scan (-sT) for unprivileged runtime", true
}

func runningAsRoot() bool {
	lookup := currentUserLookup
	if lookup == nil {
		return false
	}
	u, err := lookup()
	if err != nil || u == nil {
		return false
	}
	uid, err := strconv.Atoi(strings.TrimSpace(u.Uid))
	if err != nil {
		return false
	}
	return uid == 0
}
