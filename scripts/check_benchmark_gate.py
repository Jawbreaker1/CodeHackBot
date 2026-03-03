#!/usr/bin/env python3
import argparse
import json
import os
import sys


def canonical_low_value_key(command, args):
    cmd = (command or "").strip().lower()
    if cmd in ("ls", "list_dir"):
        return "list_dir"
    return ""


def load_json(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def detect_low_value_streak(event_path, max_streak):
    if not os.path.exists(event_path):
        return [f"missing event log: {event_path}"]
    streak_by_task = {}
    failures = []
    with open(event_path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                evt = json.loads(line)
            except json.JSONDecodeError:
                continue
            if evt.get("type") != "task_artifact":
                continue
            payload = evt.get("payload", {})
            if payload.get("type") != "command_log":
                continue
            task_id = (evt.get("task_id") or "").strip()
            key = canonical_low_value_key(payload.get("command"), payload.get("args", []))
            if not task_id:
                continue
            if not key:
                streak_by_task[task_id] = 0
                continue
            streak_by_task[task_id] = streak_by_task.get(task_id, 0) + 1
            if streak_by_task[task_id] > max_streak:
                failures.append(
                    f"{task_id}: low-value listing streak {streak_by_task[task_id]} exceeds max {max_streak}"
                )
    return failures


def collect_completed_reasons(event_path):
    reasons = []
    if not os.path.exists(event_path):
        return reasons
    with open(event_path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                evt = json.loads(line)
            except json.JSONDecodeError:
                continue
            if evt.get("type") != "task_completed":
                continue
            task_id = str(evt.get("task_id", "")).strip()
            payload = evt.get("payload", {})
            reason = str(payload.get("reason", "")).strip()
            if reason:
                reasons.append((task_id, reason))
    return reasons


def load_task_strategy_map(sessions_dir, run_id):
    plan_path = os.path.join(sessions_dir, run_id, "orchestrator", "plan", "plan.json")
    if not os.path.exists(plan_path):
        return {}
    plan = load_json(plan_path)
    out = {}
    for task in plan.get("tasks", []):
        task_id = str(task.get("task_id", "")).strip()
        if not task_id:
            continue
        out[task_id] = str(task.get("strategy", "")).strip().lower()
    return out


def is_summary_strategy(task_id, strategy):
    strategy = (strategy or "").strip().lower()
    if strategy in ("summarize_and_replan", "summary", "report_summary"):
        return True
    return (task_id or "").strip().lower() == "task-plan-summary"


def is_recon_validation_strategy(strategy):
    strategy = (strategy or "").strip().lower()
    if not strategy:
        return False
    keywords = (
        "recon",
        "hypothesis_validate",
        "validate",
        "inventory",
        "discovery",
        "service_enum",
        "vuln_mapping",
    )
    return any(keyword in strategy for keyword in keywords)


def check_summary(summary, sessions_dir, max_low_value_streak):
    failures = []
    scenarios = summary.get("scenarios", [])
    if not scenarios:
        failures.append("summary has no scenarios")
        return failures

    for scenario in scenarios:
        scenario_id = scenario.get("scenario", {}).get("id", "unknown")
        for run in scenario.get("runs", []):
            run_id = run.get("run_id", "")
            if not run_id:
                failures.append(f"{scenario_id}: missing run_id")
                continue
            if int(run.get("exit_code", 1)) != 0:
                failures.append(f"{scenario_id}/{run_id}: exit_code={run.get('exit_code')}")
            metrics = run.get("metrics", {})
            if int(metrics.get("loop_incidents", 0)) > 0:
                failures.append(f"{scenario_id}/{run_id}: loop_incidents={metrics.get('loop_incidents')}")
            reason_counts = metrics.get("failure_reason_counts", {}) or {}
            for blocked in ("assist_loop_detected", "assist_budget_exhausted"):
                if int(reason_counts.get(blocked, 0)) > 0:
                    failures.append(
                        f"{scenario_id}/{run_id}: forbidden failure reason {blocked}={reason_counts.get(blocked)}"
                    )
            event_path = os.path.join(sessions_dir, run_id, "orchestrator", "event", "event.jsonl")
            failures.extend(
                [f"{scenario_id}/{run_id}: {msg}" for msg in detect_low_value_streak(event_path, max_low_value_streak)]
            )
            strategy_by_task = load_task_strategy_map(sessions_dir, run_id)
            completed_reasons = collect_completed_reasons(event_path)
            for task_id, reason in completed_reasons:
                if reason != "assist_no_new_evidence":
                    continue
                strategy = strategy_by_task.get(task_id, "")
                if is_summary_strategy(task_id, strategy):
                    continue
                if not is_recon_validation_strategy(strategy):
                    continue
                failures.append(
                    f"{scenario_id}/{run_id}: non-summary recon/validation task {task_id} completed with reason assist_no_new_evidence (strategy={strategy or 'unknown'})"
                )
    return failures


def main():
    parser = argparse.ArgumentParser(description="Validate Sprint 36 benchmark gate thresholds")
    parser.add_argument("--summary", required=True, help="path to benchmark summary.json")
    parser.add_argument("--sessions-dir", required=True, help="sessions base directory")
    parser.add_argument("--max-low-value-streak", type=int, default=3)
    args = parser.parse_args()

    summary = load_json(args.summary)
    failures = check_summary(summary, args.sessions_dir, args.max_low_value_streak)
    if failures:
        print("quick-gate FAILED", file=sys.stderr)
        for msg in failures:
            print(f"- {msg}", file=sys.stderr)
        return 1
    print("quick-gate PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
