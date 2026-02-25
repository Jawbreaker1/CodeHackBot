package orchestrator

import (
	"fmt"
	"path/filepath"
	"strings"
)

func adaptWeakReportAction(cfg WorkerRunConfig, task TaskSpec, scopePolicy *ScopePolicy, command string, args []string, attribution targetAttribution) (string, []string, string, bool) {
	if !taskRequiresReportSynthesis(task) {
		return command, args, "", false
	}
	if !isWeakReportSynthesisCommand(command, args) {
		return command, args, "", false
	}
	target := strings.TrimSpace(attribution.Target)
	confidence := strings.TrimSpace(strings.ToLower(attribution.Confidence))
	source := strings.TrimSpace(attribution.Source)
	if target == "" {
		target = firstTaskTarget(task.Targets)
	}
	if target == "" && scopePolicy != nil {
		target = scopePolicy.FirstAllowedTarget()
	}
	if confidence == "" {
		if firstPinnedTaskTarget(task.Targets) != "" {
			confidence = "high"
			source = "task_target"
		} else {
			confidence = "medium"
			if source == "" {
				source = "scope_fallback"
			}
		}
	}
	nextCommand := "python3"
	nextArgs := buildReportSynthesisActionArgs(cfg, task, target, confidence, source)
	note := fmt.Sprintf("rewrote weak report command (%s) to local OWASP report synthesis using dependency artifacts", strings.TrimSpace(command))
	return nextCommand, nextArgs, note, true
}

func taskRequiresReportSynthesis(task TaskSpec) bool {
	titleGoalStrategy := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Title,
		task.Goal,
		task.Strategy,
	}, " ")))
	fields := []string{
		task.Title,
		task.Goal,
		strings.Join(task.DoneWhen, " "),
		strings.Join(task.FailWhen, " "),
		strings.Join(task.ExpectedArtifacts, " "),
	}
	text := strings.ToLower(strings.TrimSpace(strings.Join(fields, " ")))
	if text == "" {
		return false
	}
	if len(task.DependsOn) == 0 {
		return false
	}
	if strings.Contains(text, "owasp") {
		return true
	}
	if !containsAnySubstring(text, "report", "summary", "assessment") {
		return false
	}
	if !containsAnySubstring(titleGoalStrategy,
		"generate",
		"compile",
		"aggregate",
		"synthes",
		"summariz",
		"produce",
		"write",
		"document",
		"final",
	) {
		return false
	}
	if hasSecurityReportArtifact(task.ExpectedArtifacts) {
		return true
	}
	return containsAnySubstring(text,
		"security",
		"vulnerab",
		"finding",
		"cve",
		"exposure",
		"nmap",
		"scan",
		"exploit",
		"owasp",
		"pentest",
		"penetration",
	)
}

func hasSecurityReportArtifact(expected []string) bool {
	for _, raw := range expected {
		name := strings.ToLower(strings.TrimSpace(filepath.Base(raw)))
		if name == "" {
			continue
		}
		if strings.Contains(name, "owasp_report") {
			return true
		}
		if strings.Contains(name, "security_report") {
			return true
		}
		if strings.Contains(name, "vulnerability_report") {
			return true
		}
	}
	return false
}

func isWeakReportSynthesisCommand(command string, args []string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(command)))
	switch base {
	case "cat", "echo", "printf", "true", "false":
		return true
	case "report":
		return true
	case "nmap", "searchsploit", "msfconsole", "metasploit", "nikto", "nuclei", "curl", "wget", "nc", "netcat":
		return true
	case "python", "python3":
		return isWeakReportPythonArgs(args)
	case "bash", "sh", "zsh":
		return isWeakReportShellArgs(args)
	default:
		return false
	}
}

func isWeakReportPythonArgs(args []string) bool {
	code := inlinePythonCodeArg(args)
	if code == "" {
		return true
	}
	return !isStrongReportPythonSnippet(code)
}

func isStrongReportPythonSnippet(code string) bool {
	lower := strings.ToLower(strings.TrimSpace(code))
	if lower == "" {
		return false
	}
	if containsAnySubstring(lower,
		"http://", "https://",
		"socket.", "requests.", "urllib.", "subprocess", "nmap", "searchsploit", "msfconsole",
	) {
		return false
	}
	if !containsAnySubstring(lower, "print(", "sys.stdout.write(", "write(") {
		return false
	}
	return containsAnySubstring(lower,
		"open(", "read_text(", "os.walk(", "glob.", "pathlib.", "json.load(", "yaml.safe_load(",
	)
}

func isWeakReportShellArgs(args []string) bool {
	if len(args) < 2 {
		return false
	}
	mode := strings.TrimSpace(args[0])
	if mode != "-c" && mode != "-lc" {
		return false
	}
	body := strings.ToLower(strings.TrimSpace(args[1]))
	if body == "" {
		return false
	}
	for _, token := range shellCommandTokens(body) {
		if isNetworkSensitiveCommand(token) {
			return true
		}
	}
	if containsAnySubstring(body, "nmap ", "searchsploit", "msfconsole", "metasploit", "nikto ", "nuclei ", "curl ", "wget ", "nc ", "netcat ") {
		return true
	}
	return strings.Contains(body, "cat ") || strings.Contains(body, "echo ") || strings.Contains(body, "printf ")
}

func buildReportSynthesisActionArgs(cfg WorkerRunConfig, task TaskSpec, target, confidence, source string) []string {
	artifactRoot := BuildRunPaths(cfg.SessionsDir, cfg.RunID).ArtifactDir
	args := []string{
		"-c",
		reportSynthesisPythonScript,
		cfg.RunID,
		task.TaskID,
		strings.TrimSpace(task.Goal),
		strings.TrimSpace(target),
		strings.TrimSpace(confidence),
		strings.TrimSpace(source),
		artifactRoot,
	}
	return append(args, compactStringSlice(task.DependsOn)...)
}

const reportSynthesisPythonScript = `import datetime
import json
import os
import re
import sys

run_id = sys.argv[1].strip() if len(sys.argv) > 1 else ""
task_id = sys.argv[2].strip() if len(sys.argv) > 2 else ""
task_goal = sys.argv[3].strip() if len(sys.argv) > 3 else ""
target = sys.argv[4].strip() if len(sys.argv) > 4 else ""
target_confidence = sys.argv[5].strip() if len(sys.argv) > 5 else ""
target_source = sys.argv[6].strip() if len(sys.argv) > 6 else ""
artifact_root = sys.argv[7].strip() if len(sys.argv) > 7 else ""
deps = [value.strip() for value in sys.argv[8:] if value.strip()]

if not deps and artifact_root and os.path.isdir(artifact_root):
    for name in sorted(os.listdir(artifact_root)):
        dep_path = os.path.join(artifact_root, name)
        if not os.path.isdir(dep_path):
            continue
        if task_id and name == task_id:
            continue
        deps.append(name)

def collect_files(root_dir, dep_ids):
    files = []
    for dep in dep_ids:
        dep_dir = os.path.join(root_dir, dep)
        if not os.path.isdir(dep_dir):
            continue
        for walk_root, _, names in os.walk(dep_dir):
            for name in sorted(names):
                files.append(os.path.join(walk_root, name))
    deduped = sorted(set(files))
    return deduped[:200]

def collect_files_by_dep(root_dir, dep_ids):
    out = {}
    for dep in dep_ids:
        dep_dir = os.path.join(root_dir, dep)
        if not os.path.isdir(dep_dir):
            continue
        files = []
        for walk_root, _, names in os.walk(dep_dir):
            for name in sorted(names):
                files.append(os.path.join(walk_root, name))
        out[dep] = sorted(set(files))
    return out

def read_text(path, limit=262144):
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as handle:
            return handle.read(limit)
    except Exception as err:
        return "read_error: %s" % err

def rel_path(path):
    if not artifact_root:
        return path
    try:
        return os.path.relpath(path, artifact_root)
    except Exception:
        return path

def load_json(path):
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as handle:
            return json.load(handle)
    except Exception:
        return None

def first_nonempty_line(text):
    for raw in text.splitlines():
        line = raw.strip()
        if line:
            return line
    return ""

def normalize_target_tokens(raw_target):
    tokens = set()
    value = (raw_target or "").strip().lower()
    if not value:
        return tokens
    tokens.add(value)
    base = value.split("/")[0].strip()
    if base:
        tokens.add(base)
    for prefix in ("http://", "https://"):
        if base.startswith(prefix):
            host = base[len(prefix):].split("/")[0].strip()
            if host:
                tokens.add(host)
    if ":" in base and not re.match(r"^\d{1,3}(?:\.\d{1,3}){3}$", base):
        host = base.split(":", 1)[0].strip()
        if host:
            tokens.add(host)
    return tokens

def cve_line_score(line, target_tokens, file_target_relevant):
    lower = line.lower()
    score = 0
    confidence = "low"
    vuln_markers = (
        "vulnerable",
        "vuln",
        "exploit",
        "exposed",
        "nse:",
        "state: vulnerable",
        "host appears vulnerable",
    )
    for marker in vuln_markers:
        if marker in lower:
            score += 4
            break
    if file_target_relevant:
        score += 2
    if target_tokens and any(token and token in lower for token in target_tokens):
        score += 2
    if "nmap" in lower or "port " in lower or "/tcp" in lower or "/udp" in lower:
        score += 1

    cve_mentions = len(re.findall(r"\bCVE[-_ ]?\d{4}[-_]\d{4,}\b", line, re.IGNORECASE))
    if cve_mentions >= 3:
        score -= 3

    noise_markers = (
        "reference",
        "references",
        "advisory",
        "bulletin",
        "feed",
        "cpe:",
        "nvd.nist",
        "mitre",
        "cvss",
        "vector:",
        "https://",
        "http://",
    )
    for marker in noise_markers:
        if marker in lower:
            score -= 2
            break

    if score >= 6:
        confidence = "high"
    elif score >= 3:
        confidence = "medium"
    return score, confidence

def summarize_findings(paths):
    cve_pattern = re.compile(r"\bCVE[-_ ]?(\d{4})[-_](\d{4,})\b", re.IGNORECASE)
    findings = {}
    target_tokens = normalize_target_tokens(target)
    for path in paths:
        text = read_text(path)
        lower_text = text.lower()
        lower_path = path.lower()
        file_target_relevant = bool(target_tokens and any(token in lower_text or token in lower_path for token in target_tokens))
        for raw_line in text.splitlines():
            line = raw_line.strip()
            if not line:
                continue
            score, confidence = cve_line_score(line, target_tokens, file_target_relevant)
            for year, ident in cve_pattern.findall(line):
                cve = "CVE-%s-%s" % (year, ident)
                snippet = line
                if len(snippet) > 220:
                    snippet = snippet[:217] + "..."
                findings.setdefault(cve, [])
                findings[cve].append({
                    "path": path,
                    "snippet": snippet,
                    "score": score,
                    "confidence": confidence,
                })

    reduced = {}
    for cve, entries in findings.items():
        if not entries:
            continue
        deduped = []
        seen = set()
        for entry in entries:
            key = (entry["path"], entry["snippet"].lower())
            if key in seen:
                continue
            seen.add(key)
            deduped.append(entry)
        deduped.sort(key=lambda item: (-item["score"], item["path"], item["snippet"]))
        high = [item for item in deduped if item["score"] >= 6]
        medium = [item for item in deduped if item["score"] >= 3]
        if high:
            chosen = high
        elif medium:
            chosen = medium
        else:
            chosen = deduped
        reduced[cve] = chosen[:3]
    return reduced

def summarize_execution(paths):
    port_line_pattern = re.compile(r"^\d+/(tcp|udp)\s+\S+", re.IGNORECASE)
    open_port_pattern = re.compile(r"^\d+/(tcp|udp)\s+open\b", re.IGNORECASE)
    summary = {
        "artifact_files": len(paths),
        "nmap_reports": 0,
        "port_lines": 0,
        "open_ports": 0,
        "vuln_signal_files": 0,
    }
    for path in paths:
        text = read_text(path)
        lower = text.lower()
        if "nmap scan report for" in lower:
            summary["nmap_reports"] += 1
        if "cve-" in lower or "vuln" in lower:
            summary["vuln_signal_files"] += 1
        for raw_line in text.splitlines():
            line = raw_line.strip()
            if not line:
                continue
            if port_line_pattern.match(line):
                summary["port_lines"] += 1
            if open_port_pattern.match(line):
                summary["open_ports"] += 1
    return summary

def summarize_task_trace(dep_files):
    traces = []
    notable = []
    priority_names = {
        "admin_attempt_result.json",
        "admin_access_proof.txt",
        "router_fingerprint.json",
        "login_state_before.json",
        "endpoint_probe.txt",
    }
    for dep in deps:
        files = dep_files.get(dep, [])
        trace = {
            "task_id": dep,
            "artifact_count": len(files),
            "key_artifacts": [],
            "observations": [],
        }
        seen_obs = set()
        for path in files:
            name = os.path.basename(path)
            lower_name = name.lower()
            if name in priority_names and len(trace["key_artifacts"]) < 6:
                trace["key_artifacts"].append(rel_path(path))
            if lower_name == "admin_access_proof.txt":
                proof_line = first_nonempty_line(read_text(path, 4096))
                if proof_line:
                    obs = "admin access proof status: %s" % proof_line
                    if obs not in seen_obs:
                        trace["observations"].append(obs)
                        seen_obs.add(obs)
            if lower_name == "endpoint_probe.txt":
                probe_lines = [line.strip() for line in read_text(path, 8192).splitlines() if line.strip()]
                if probe_lines:
                    codes = []
                    for line in probe_lines:
                        parts = line.split()
                        if parts:
                            code = parts[-1]
                            if re.match(r"^\d{3}$", code):
                                codes.append(code)
                    unique_codes = sorted(set(codes))
                    obs = "endpoint probe: %d path checks (status codes: %s)" % (
                        len(probe_lines),
                        ", ".join(unique_codes) if unique_codes else "n/a",
                    )
                    if obs not in seen_obs:
                        trace["observations"].append(obs)
                        seen_obs.add(obs)
            if lower_name.endswith(".json"):
                data = load_json(path)
                if not isinstance(data, dict):
                    continue
                if isinstance(data.get("attempts"), list) and "success" in data:
                    attempts = data.get("attempts") or []
                    method = str(data.get("method") or "unknown")
                    success = bool(data.get("success"))
                    stop_reason = str(data.get("stop_reason") or "not_provided")
                    rejected = 0
                    maybe_success = 0
                    for item in attempts:
                        if not isinstance(item, dict):
                            continue
                        outcome = item.get("suspected_success")
                        if outcome is True:
                            maybe_success += 1
                        elif outcome is False:
                            rejected += 1
                    obs = "attempt workflow: method=%s, attempts=%d, success=%s, stop_reason=%s, rejected=%d, suspected_success=%d" % (
                        method,
                        len(attempts),
                        "true" if success else "false",
                        stop_reason,
                        rejected,
                        maybe_success,
                    )
                    if obs not in seen_obs:
                        trace["observations"].append(obs)
                        seen_obs.add(obs)
                        notable.append("task %s: %s" % (dep, obs))
                if isinstance(data.get("form_actions"), list) or isinstance(data.get("candidate_auth_endpoints"), list):
                    actions = data.get("form_actions") or []
                    endpoints = data.get("candidate_auth_endpoints") or []
                    model = str(data.get("title") or "unknown")
                    obs = "auth surface fingerprint: model/title=%s, form_actions=%d, candidate_endpoints=%d" % (
                        model,
                        len(actions),
                        len(endpoints),
                    )
                    if obs not in seen_obs:
                        trace["observations"].append(obs)
                        seen_obs.add(obs)
                if all(key in data for key in ("error_num", "error_status", "lock_time")):
                    obs = "login state snapshot: error_num=%s, error_status=%s, lock_time=%s" % (
                        data.get("error_num"),
                        data.get("error_status"),
                        data.get("lock_time"),
                    )
                    if obs not in seen_obs:
                        trace["observations"].append(obs)
                        seen_obs.add(obs)
        if not trace["key_artifacts"]:
            for path in files[:4]:
                trace["key_artifacts"].append(rel_path(path))
        traces.append(trace)
    return traces, notable

files = collect_files(artifact_root, deps)
dep_files = collect_files_by_dep(artifact_root, deps)
findings = summarize_findings(files)
execution = summarize_execution(files)
task_traces, notable_results = summarize_task_trace(dep_files)
finding_count = len(findings)

if finding_count > 0:
    outcome = "Evidence-backed vulnerabilities identified (%d CVE IDs)." % finding_count
    assessment_confidence = "high"
else:
    outcome = "No evidence-backed vulnerabilities identified in this run."
    if execution["nmap_reports"] > 0:
        assessment_confidence = "medium"
    else:
        assessment_confidence = "low"

print("# OWASP-Style Security Assessment Report")
print("")
print("## Scope")
if target:
    print("- In-scope target: %s" % target)
else:
    print("- In-scope target: not provided")
if task_goal:
    print("- Report objective: %s" % task_goal)
else:
    print("- Report objective: not provided")
print("- Attribution confidence: %s" % (target_confidence or "unknown"))
print("- Attribution source: %s" % (target_source or "unknown"))
print("- Run ID: %s" % (run_id or "unknown"))
print("- Report task ID: %s" % (task_id or "unknown"))
if deps:
    print("- Source tasks: %s" % ", ".join(deps))
else:
    print("- Source tasks: none")
print("- Generated at (UTC): %s" % datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S"))
print("")
print("## Executive Summary")
print("- Outcome: %s" % outcome)
print("- Assessment confidence: %s" % assessment_confidence)
print("- Assessment model: network-based, unauthenticated evidence synthesis from prior task artifacts.")
if notable_results:
    for line in notable_results[:3]:
        print("- Observed execution result: %s" % line)
if finding_count == 0:
    print("- Interpretation: no observed CVE evidence in collected artifacts (this is not proof of absence).")
print("")
print("## Test Execution Summary")
print("- Source tasks analyzed: %s" % (", ".join(deps) if deps else "none"))
print("- Artifact files analyzed: %d" % execution["artifact_files"])
print("- Nmap host reports observed: %d" % execution["nmap_reports"])
print("- Service/port evidence lines observed: %d (open ports: %d)" % (execution["port_lines"], execution["open_ports"]))
print("- Vulnerability-signal files observed: %d" % execution["vuln_signal_files"])
print("")
print("## Methodology")
print("- Consolidated evidence artifacts generated by prerequisite tasks.")
print("- Extracted service and vulnerability indicators from command logs and derived artifacts.")
print("- Mapped explicit CVE identifiers to supporting evidence lines.")
print("- Summarized per-task execution traces and structured outcome markers from dependency artifacts.")
print("")
print("## Task Execution Trace")
if not task_traces:
    print("- No dependency task traces were available for summarization.")
else:
    for trace in task_traces:
        print("### Task %s" % trace["task_id"])
        print("- Artifacts discovered: %d" % trace["artifact_count"])
        if trace["key_artifacts"]:
            print("- Key artifacts:")
            for path in trace["key_artifacts"][:6]:
                print("  - %s" % path)
        else:
            print("- Key artifacts: none")
        if trace["observations"]:
            for obs in trace["observations"][:4]:
                print("- Observed result: %s" % obs)
        else:
            print("- Observed result: no structured outcome markers parsed from this task's artifacts.")
        print("")
print("## Findings")
if not findings:
    print("- No explicit CVE identifiers were found in dependency artifacts.")
else:
    for cve in sorted(findings.keys()):
        print("### %s" % cve)
        entries = findings[cve]
        for item in entries:
            print("- Evidence file: %s" % item["path"])
            print("- Evidence confidence: %s" % item.get("confidence", "unknown"))
            print("- Evidence excerpt: %s" % item["snippet"])
        print("")
print("## Limitations")
print("- Results are limited to network-visible behavior from the analyzed artifacts.")
print("- No authenticated testing, exploitation, persistence, or data exfiltration actions were performed.")
print("- Device identity can change over time (for example dynamic IP/MAC); attribution should be re-verified per run.")
print("- Structured execution traces rely on dependency artifacts and can omit steps that were not logged.")
print("")
print("## Evidence")
if not files:
    print("- No dependency artifacts were discovered for report synthesis.")
else:
    for path in files[:40]:
        print("- %s" % path)
    if len(files) > 40:
        print("- ... %d additional artifact files omitted for brevity" % (len(files) - 40))
print("")
print("## Remediation")
if not findings:
    print("- No finding-specific remediation generated because no evidence-backed vulnerabilities were identified.")
    print("- Optional hardening: maintain current patch levels, minimize exposed services, and repeat the same bounded scan profile during future checks.")
else:
    print("- Prioritize remediation for identified CVEs based on exploitability and exposure.")
    print("- Patch affected firmware/services, re-test with the same bounded scan profile, and document closure evidence.")
    print("- Add compensating controls (ACLs, segmentation, management interface restrictions) until patches are deployed.")
`
