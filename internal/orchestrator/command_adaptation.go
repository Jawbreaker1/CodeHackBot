package orchestrator

import (
	"fmt"
	"net"
	"strings"
)

func adaptCommandForRuntime(scopePolicy *ScopePolicy, command string, args []string) (string, []string, string, bool) {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	if scopePolicy == nil || !strings.EqualFold(command, "nmap") {
		return command, args, "", false
	}
	if hasNmapFlag(args, "-sn") {
		return command, args, "", false
	}
	if !hasNmapFingerprintArgs(args) {
		return command, args, "", false
	}
	target, ok := firstBroadCIDRTarget(scopePolicy.extractTargets(command, args))
	if !ok {
		return command, args, "", false
	}
	adapted := buildNmapDiscoveryArgs(args, target)
	if stringSlicesEqual(args, adapted) {
		return command, args, "", false
	}
	note := fmt.Sprintf("adapted nmap for broad target %s: switched to host discovery (-sn) to avoid timeout; run targeted service scans on discovered hosts next", target)
	return command, adapted, note, true
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
