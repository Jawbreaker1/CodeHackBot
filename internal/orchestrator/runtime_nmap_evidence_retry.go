package orchestrator

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func shouldRetryNmapForHostTimeout(command string, args []string, output []byte, runErr error) bool {
	if runErr != nil {
		return false
	}
	if !isNmapCommand(command) || !nmapRequiresActionableEvidence(args) {
		return false
	}
	return nmapOutputIndicatesHostTimeout(output)
}

func buildNmapEvidenceRetryArgs(args []string, remaining time.Duration) ([]string, string, bool) {
	if !nmapRequiresActionableEvidence(args) {
		return args, "", false
	}
	profile := selectNmapGuardrailProfile(args)
	targetHostTimeout := "75s"
	targetMaxRetries := "2"
	targetMaxRate := "900"
	if profile.name == "vuln_mapping" {
		targetHostTimeout = "120s"
		targetMaxRetries = "3"
		targetMaxRate = "600"
	}
	if bounded := fitRetryHostTimeoutToRemaining(targetHostTimeout, remaining); bounded != "" {
		targetHostTimeout = bounded
	} else if remaining > 0 {
		return args, "", false
	}
	next := setNmapOptionValue(args, "--host-timeout", targetHostTimeout)
	next = setNmapOptionValue(next, "--max-retries", targetMaxRetries)
	next = setNmapOptionValue(next, "--max-rate", targetMaxRate)
	if stringSlicesEqual(next, args) {
		return args, "", false
	}
	note := fmt.Sprintf("nmap host-timeout detected; retrying once with relaxed evidence profile (--host-timeout %s --max-retries %s --max-rate %s)", targetHostTimeout, targetMaxRetries, targetMaxRate)
	return next, note, true
}

func remainingContextDuration(ctx context.Context) time.Duration {
	if ctx == nil {
		return 0
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	return time.Until(deadline)
}

func fitRetryHostTimeoutToRemaining(target string, remaining time.Duration) string {
	if remaining <= 0 {
		return target
	}
	targetDur, err := time.ParseDuration(strings.TrimSpace(target))
	if err != nil || targetDur <= 0 {
		return target
	}
	// Reserve budget for process startup/teardown and result handling.
	available := remaining - 20*time.Second
	if available < 15*time.Second {
		return ""
	}
	if targetDur > available {
		targetDur = available
	}
	targetDur = targetDur.Round(time.Second)
	if targetDur < 15*time.Second {
		return ""
	}
	return targetDur.String()
}

func setNmapOptionValue(args []string, option, value string) []string {
	option = strings.ToLower(strings.TrimSpace(option))
	if option == "" || strings.TrimSpace(value) == "" {
		return append([]string{}, args...)
	}
	out := make([]string, 0, len(args)+2)
	set := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		lower := strings.ToLower(arg)
		if lower == option {
			if !set {
				out = append(out, arg)
				out = append(out, value)
				set = true
			}
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(lower, option+"=") {
			if !set {
				out = append(out, option+"="+value)
				set = true
			}
			continue
		}
		out = append(out, arg)
	}
	if !set {
		out = append(out, option, value)
	}
	return out
}

func mergeRetryOutput(first, second []byte, note string) []byte {
	const divider = "\n--- nmap retry ---\n"
	out := append([]byte{}, first...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	if strings.TrimSpace(note) != "" {
		out = append(out, []byte(note)...)
		out = append(out, '\n')
	}
	out = append(out, []byte(divider)...)
	out = append(out, second...)
	return out
}

func validateCommandOutputEvidence(task TaskSpec, command string, args []string, output []byte) error {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	lower := strings.ToLower(string(output))
	if outputLooksLikeUsageOnly(lower) {
		return fmt.Errorf("%s output indicates usage/help only; command lacks concrete inputs", base)
	}
	if !isNmapCommand(command) || !nmapRequiresActionableEvidence(args) {
		if base == "msfconsole" {
			if !hasMetasploitExecutionArg(args) {
				return fmt.Errorf("msfconsole command missing non-interactive execution arg (-x/--exec/-e/-r)")
			}
			if containsAnySubstring(lower, "parse error", "unmatched quote", "invalid option") {
				return fmt.Errorf("msfconsole output indicates script parsing/option failure")
			}
		}
		if taskLikelyLocalFileWorkflow(task) && base == "john" {
			if strings.Contains(lower, "no password hashes loaded") {
				return fmt.Errorf("john reported no password hashes loaded; hash/format pipeline is invalid")
			}
		}
		if taskLikelyLocalFileWorkflow(task) && base == "fcrackzip" {
			if strings.TrimSpace(string(output)) == "" {
				return fmt.Errorf("fcrackzip produced no output evidence; require verbose evidence capture")
			}
		}
		if taskLikelyLocalFileWorkflow(task) && base == "zip" && len(args) == 0 {
			return fmt.Errorf("zip command missing archive arguments in local archive workflow")
		}
		if taskLikelyLocalFileWorkflow(task) && base == "unzip" && len(args) == 0 {
			return fmt.Errorf("unzip command missing archive arguments in local archive workflow")
		}
		return nil
	}
	if nmapOutputIndicatesHostTimeout(output) && !nmapOutputContainsActionableEvidence(output) {
		return fmt.Errorf("nmap output indicates host timeout without actionable service/vulnerability evidence")
	}
	return nil
}

func outputLooksLikeUsageOnly(lowerOutput string) bool {
	lower := strings.ToLower(strings.TrimSpace(lowerOutput))
	if lower == "" {
		return false
	}
	if !containsAnySubstring(
		lower,
		"usage:",
		"try --help",
		"use --help",
		"invalid option",
		"you have to specify",
		"missing operand",
	) {
		return false
	}
	if containsAnySubstring(
		lower,
		"password found",
		"password recovered",
		"password hash cracked",
		"archive extracted",
		"proof_of_access",
	) {
		return false
	}
	if outputContainsMeaningfulNonUsageLine(lower) {
		return false
	}
	return true
}

func outputContainsMeaningfulNonUsageLine(lowerOutput string) bool {
	lines := strings.Split(lowerOutput, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if containsAnySubstring(
			line,
			"usage:",
			"try --help",
			"use --help",
			"invalid option",
			"you have to specify",
			"missing operand",
			"for help",
			"options:",
			"error:",
			"unknown option",
			"unrecognized option",
			"requires an argument",
			"requires a value",
			"missing required",
		) {
			continue
		}
		return true
	}
	return false
}

func hasMetasploitExecutionArg(args []string) bool {
	for i := 0; i < len(args); i++ {
		flag := strings.ToLower(strings.TrimSpace(args[i]))
		if flag == "-r" {
			return true
		}
		if flag == "-x" || flag == "--exec" || flag == "-e" {
			if i+1 >= len(args) {
				return false
			}
			payload := strings.TrimSpace(args[i+1])
			return payload != ""
		}
	}
	return false
}
