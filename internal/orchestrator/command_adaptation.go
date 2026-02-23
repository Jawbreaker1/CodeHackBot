package orchestrator

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	nmapGuardrailHostTimeoutDiscovery = "10s"
	nmapGuardrailHostTimeoutStandard  = "20s"
	nmapGuardrailHostTimeoutService   = "45s"
	nmapGuardrailHostTimeoutVuln      = "90s"
	nmapGuardrailMaxRetriesDiscovery  = "1"
	nmapGuardrailMaxRetriesStandard   = "1"
	nmapGuardrailMaxRetriesService    = "2"
	nmapGuardrailMaxRetriesVuln       = "2"
	nmapGuardrailMaxRateDiscovery     = "1500"
	nmapGuardrailMaxRateStandard      = "1500"
	nmapGuardrailMaxRateService       = "1200"
	nmapGuardrailMaxRateVuln          = "800"
)

type nmapGuardrailProfile struct {
	name        string
	hostTimeout string
	maxRetries  string
	maxRate     string
}

var currentUserLookup = user.Current
var nmapScriptDirs = []string{
	"/usr/share/nmap/scripts",
	"/usr/local/share/nmap/scripts",
}

func adaptCommandForRuntime(scopePolicy *ScopePolicy, command string, args []string) (string, []string, string, bool) {
	command = strings.TrimSpace(command)
	args = normalizeArgs(args)
	if !isNmapCommand(command) {
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
	if scopePolicy != nil && !hasNmapFlag(args, "-sn") && (hasNmapFingerprintArgs(args) || hasNmapAggressiveRangeArgs(args)) {
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
	if scopePolicy != nil && hasNmapFlag(args, "-sn") {
		targets := scopePolicy.extractTargets(command, args)
		if nextArgs, note, ok := capBroadLoopbackDiscoveryTarget(args, targets); ok {
			args = nextArgs
			adapted = true
			if note != "" {
				notes = append(notes, note)
			}
		}
	}
	if nextArgs, note, ok := sanitizeNmapScriptSelection(args); ok {
		args = nextArgs
		adapted = true
		if note != "" {
			notes = append(notes, note)
		}
	}
	if nextArgs, note, ok := applyNmapEvidenceBounds(args); ok {
		args = nextArgs
		adapted = true
		if note != "" {
			notes = append(notes, note)
		}
	}
	if nextArgs, note, ok := ensureNmapRuntimeGuardrails(args); ok {
		args = nextArgs
		adapted = true
		if note != "" {
			notes = append(notes, note)
		}
	}
	return command, args, strings.Join(notes, "; "), adapted
}

func isNmapCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	return strings.EqualFold(filepath.Base(command), "nmap")
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

func hasNmapAggressiveRangeArgs(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		switch arg {
		case "-p-":
			return true
		case "-p":
			if i+1 < len(args) && strings.TrimSpace(args[i+1]) == "-" {
				return true
			}
		case "--top-ports":
			if i+1 < len(args) {
				if value, err := strconv.Atoi(strings.TrimSpace(args[i+1])); err == nil && value > 100 {
					return true
				}
			}
		default:
			if strings.HasPrefix(arg, "--top-ports=") {
				value := strings.TrimSpace(strings.TrimPrefix(arg, "--top-ports="))
				if n, err := strconv.Atoi(value); err == nil && n > 100 {
					return true
				}
			}
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

func capBroadLoopbackDiscoveryTarget(args, targets []string) ([]string, string, bool) {
	for _, target := range targets {
		cappedTarget, ok := cappedLoopbackDiscoveryTarget(target)
		if !ok {
			continue
		}
		nextArgs := buildNmapDiscoveryArgs(args, cappedTarget)
		if stringSlicesEqual(args, nextArgs) {
			continue
		}
		note := fmt.Sprintf("capped broad loopback host discovery target %s to %s to limit output volume", target, cappedTarget)
		return nextArgs, note, true
	}
	return args, "", false
}

func cappedLoopbackDiscoveryTarget(target string) (string, bool) {
	target = strings.TrimSpace(target)
	if target == "" || !strings.Contains(target, "/") {
		return "", false
	}
	ip, network, err := net.ParseCIDR(target)
	if err != nil || ip == nil || network == nil {
		return "", false
	}
	if !ip.IsLoopback() {
		return "", false
	}
	ones, bits := network.Mask.Size()
	if bits == 32 && ones < 24 {
		return "127.0.0.1", true
	}
	if bits == 128 && ones < 120 {
		return "::1", true
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

func applyNmapEvidenceBounds(args []string) ([]string, string, bool) {
	serviceLike := nmapArgsSuggestServiceEnumeration(args)
	vulnLike := nmapArgsContainScriptToken(args, "vuln")
	if !serviceLike && !vulnLike {
		return args, "", false
	}
	changed := false
	notes := make([]string, 0, 4)
	out := append([]string{}, args...)
	if next, note, ok := capNmapPortBreadth(out, 20); ok {
		out = next
		changed = true
		if note != "" {
			notes = append(notes, note)
		}
	}
	if serviceLike {
		if next, note, ok := ensureNmapVersionLight(out); ok {
			out = next
			changed = true
			if note != "" {
				notes = append(notes, note)
			}
		}
	}
	if vulnLike {
		if next, note, ok := rewriteVulnScriptToSafeSubset(out); ok {
			out = next
			changed = true
			if note != "" {
				notes = append(notes, note)
			}
		}
		if !hasNmapOption(out, "--script-timeout") {
			out = append(out, "--script-timeout", "20s")
			changed = true
			notes = append(notes, "bounded nmap script runtime with --script-timeout 20s")
		}
	}
	if !changed {
		return args, "", false
	}
	return out, strings.Join(notes, "; "), true
}

func capNmapPortBreadth(args []string, maxTopPorts int) ([]string, string, bool) {
	if maxTopPorts <= 0 {
		return args, "", false
	}
	out := make([]string, 0, len(args)+2)
	changed := false
	alreadyHasTopPorts := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		switch lower {
		case "--top-ports":
			out = append(out, arg)
			if i+1 < len(args) {
				value := strings.TrimSpace(args[i+1])
				if n, err := strconv.Atoi(value); err == nil {
					if n > maxTopPorts {
						out = append(out, strconv.Itoa(maxTopPorts))
						changed = true
					} else {
						out = append(out, value)
					}
				} else {
					out = append(out, value)
				}
				i++
			}
			alreadyHasTopPorts = true
			continue
		case "-p-":
			changed = true
			if !alreadyHasTopPorts {
				out = append(out, "--top-ports", strconv.Itoa(maxTopPorts))
				alreadyHasTopPorts = true
			}
			continue
		case "-p":
			if i+1 < len(args) && strings.TrimSpace(args[i+1]) == "-" {
				changed = true
				if !alreadyHasTopPorts {
					out = append(out, "--top-ports", strconv.Itoa(maxTopPorts))
					alreadyHasTopPorts = true
				}
				i++
				continue
			}
		}
		if strings.HasPrefix(lower, "--top-ports=") {
			value := strings.TrimSpace(strings.TrimPrefix(lower, "--top-ports="))
			if n, err := strconv.Atoi(value); err == nil {
				if n > maxTopPorts {
					out = append(out, "--top-ports="+strconv.Itoa(maxTopPorts))
					changed = true
				} else {
					out = append(out, arg)
				}
				alreadyHasTopPorts = true
				continue
			}
		}
		out = append(out, arg)
	}
	if !alreadyHasTopPorts && !hasNmapExplicitPortSelection(out) {
		out = append([]string{"--top-ports", strconv.Itoa(maxTopPorts)}, out...)
		changed = true
	}
	if !changed {
		return args, "", false
	}
	return out, fmt.Sprintf("bounded nmap port breadth to top %d ports for completion-oriented evidence", maxTopPorts), true
}

func hasNmapExplicitPortSelection(args []string) bool {
	for i := 0; i < len(args); i++ {
		lower := strings.ToLower(strings.TrimSpace(args[i]))
		if lower == "-p" || lower == "-p-" || lower == "--top-ports" {
			return true
		}
		if strings.HasPrefix(lower, "-p") && len(lower) > 2 {
			return true
		}
		if strings.HasPrefix(lower, "--top-ports=") {
			return true
		}
	}
	return false
}

func ensureNmapVersionLight(args []string) ([]string, string, bool) {
	if !hasNmapFlag(args, "-sv") {
		return args, "", false
	}
	if hasNmapOption(args, "--version-light") || hasNmapOption(args, "--version-all") || hasNmapOption(args, "--version-trace") {
		return args, "", false
	}
	out := append([]string{}, args...)
	out = append([]string{"--version-light"}, out...)
	return out, "enabled --version-light to improve service-enum completion time", true
}

func rewriteVulnScriptToSafeSubset(args []string) ([]string, string, bool) {
	out := make([]string, 0, len(args))
	changed := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		if lower == "--script" && i+1 < len(args) {
			value := strings.TrimSpace(args[i+1])
			if strings.EqualFold(value, "vuln") {
				out = append(out, arg, "vuln and safe")
				changed = true
			} else {
				out = append(out, arg, value)
			}
			i++
			continue
		}
		if strings.HasPrefix(lower, "--script=") {
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--script="))
			if strings.EqualFold(value, "vuln") {
				out = append(out, "--script=vuln and safe")
				changed = true
			} else {
				out = append(out, arg)
			}
			continue
		}
		out = append(out, arg)
	}
	if !changed {
		return args, "", false
	}
	return out, "rewrote --script vuln to 'vuln and safe' to reduce long-running intrusive NSE checks", true
}

func ensureNmapRuntimeGuardrails(args []string) ([]string, string, bool) {
	filtered := make([]string, 0, len(args))
	removedReverseDNS := false
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		if trimmed == "-R" || strings.EqualFold(trimmed, "--system-dns") {
			removedReverseDNS = true
			continue
		}
		filtered = append(filtered, trimmed)
	}
	profile := selectNmapGuardrailProfile(filtered)

	prefix := make([]string, 0, 7)
	if !hasNmapFlag(filtered, "-n") {
		prefix = append(prefix, "-n")
	}
	if !hasNmapOption(filtered, "--host-timeout") {
		prefix = append(prefix, "--host-timeout", profile.hostTimeout)
	}
	if !hasNmapOption(filtered, "--max-retries") {
		prefix = append(prefix, "--max-retries", profile.maxRetries)
	}
	if !hasNmapOption(filtered, "--max-rate") {
		prefix = append(prefix, "--max-rate", profile.maxRate)
	}

	out := filtered
	if len(prefix) > 0 {
		out = append(prefix, filtered...)
	}
	if !removedReverseDNS && len(prefix) == 0 {
		return args, "", false
	}

	notes := make([]string, 0, 2)
	if len(prefix) > 0 {
		notes = append(notes, fmt.Sprintf("enforced nmap runtime guardrails profile=%s (%s)", profile.name, strings.Join(prefix, " ")))
	}
	if removedReverseDNS {
		notes = append(notes, "removed reverse-DNS flags (-R/--system-dns) to keep scans DNS-safe")
	}
	return out, strings.Join(notes, "; "), true
}

func selectNmapGuardrailProfile(args []string) nmapGuardrailProfile {
	profile := nmapGuardrailProfile{
		name:        "standard",
		hostTimeout: nmapGuardrailHostTimeoutStandard,
		maxRetries:  nmapGuardrailMaxRetriesStandard,
		maxRate:     nmapGuardrailMaxRateStandard,
	}
	if hasNmapFlag(args, "-sn") {
		return nmapGuardrailProfile{
			name:        "discovery",
			hostTimeout: nmapGuardrailHostTimeoutDiscovery,
			maxRetries:  nmapGuardrailMaxRetriesDiscovery,
			maxRate:     nmapGuardrailMaxRateDiscovery,
		}
	}
	if nmapArgsContainScriptToken(args, "vuln") {
		return nmapGuardrailProfile{
			name:        "vuln_mapping",
			hostTimeout: nmapGuardrailHostTimeoutVuln,
			maxRetries:  nmapGuardrailMaxRetriesVuln,
			maxRate:     nmapGuardrailMaxRateVuln,
		}
	}
	if nmapArgsSuggestServiceEnumeration(args) {
		return nmapGuardrailProfile{
			name:        "service_enum",
			hostTimeout: nmapGuardrailHostTimeoutService,
			maxRetries:  nmapGuardrailMaxRetriesService,
			maxRate:     nmapGuardrailMaxRateService,
		}
	}
	return profile
}

func nmapArgsContainScriptToken(args []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return false
	}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		if lower == "--script" && i+1 < len(args) {
			values := strings.Split(strings.ToLower(strings.TrimSpace(args[i+1])), ",")
			for _, value := range values {
				if strings.TrimSpace(value) == needle {
					return true
				}
			}
			i++
			continue
		}
		if strings.HasPrefix(lower, "--script=") {
			values := strings.Split(strings.TrimPrefix(lower, "--script="), ",")
			for _, value := range values {
				if strings.TrimSpace(value) == needle {
					return true
				}
			}
		}
	}
	return false
}

func nmapArgsSuggestServiceEnumeration(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		switch arg {
		case "-sv", "-a", "--version-all", "--version-light", "--version-trace":
			return true
		}
	}
	return false
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

func sanitizeNmapScriptSelection(args []string) ([]string, string, bool) {
	out := make([]string, 0, len(args))
	removed := make([]string, 0, 2)
	changed := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		switch {
		case lower == "--script":
			if i+1 >= len(args) {
				out = append(out, arg)
				continue
			}
			next := strings.TrimSpace(args[i+1])
			cleaned, dropped, ok := sanitizeNmapScriptValue(next)
			if ok {
				changed = true
				removed = append(removed, dropped...)
				if cleaned != "" {
					out = append(out, arg, cleaned)
				}
			} else {
				out = append(out, arg, next)
			}
			i++
		case strings.HasPrefix(lower, "--script="):
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--script="))
			cleaned, dropped, ok := sanitizeNmapScriptValue(value)
			if ok {
				changed = true
				removed = append(removed, dropped...)
				if cleaned != "" {
					out = append(out, "--script="+cleaned)
				}
			} else {
				out = append(out, arg)
			}
		default:
			out = append(out, arg)
		}
	}
	if !changed {
		return args, "", false
	}
	note := "removed unsupported nmap --script entries"
	if len(removed) > 0 {
		note += ": " + strings.Join(dedupeStrings(removed), ", ")
	}
	return out, note, true
}

func sanitizeNmapScriptValue(value string) (string, []string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil, false
	}
	if strings.ContainsAny(trimmed, " ()&|!;") {
		// Keep complex script expressions untouched.
		return value, nil, false
	}
	parts := strings.Split(trimmed, ",")
	kept := make([]string, 0, len(parts))
	removed := make([]string, 0, len(parts))
	changed := false
	for _, raw := range parts {
		token := strings.TrimSpace(raw)
		if token == "" {
			changed = true
			continue
		}
		if isAllowedNmapScriptToken(token) {
			kept = append(kept, token)
			continue
		}
		removed = append(removed, token)
		changed = true
	}
	if !changed {
		return trimmed, nil, false
	}
	return strings.Join(kept, ","), removed, true
}

func isAllowedNmapScriptToken(token string) bool {
	lower := strings.ToLower(strings.TrimSpace(token))
	if lower == "" {
		return false
	}
	if isKnownNmapScriptCategory(lower) {
		return true
	}
	if strings.ContainsAny(lower, "*?[]{}") {
		return true
	}
	if strings.Contains(lower, "/") {
		return true
	}
	name := lower
	if !strings.HasSuffix(name, ".nse") {
		name += ".nse"
	}
	for _, dir := range nmapScriptDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

func isKnownNmapScriptCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "all", "auth", "broadcast", "brute", "default", "discovery", "dos", "exploit", "external", "fuzzer", "intrusive", "malware", "safe", "version", "vuln":
		return true
	default:
		return false
	}
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
