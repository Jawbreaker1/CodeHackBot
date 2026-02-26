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
            payload = evt.get("payload", {})
            reason = str(payload.get("reason", "")).strip()
            if reason:
                reasons.append(reason)
    return reasons


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
            completed_reasons = collect_completed_reasons(event_path)
            if any(reason == "assist_no_new_evidence" for reason in completed_reasons):
                failures.append(
                    f"{scenario_id}/{run_id}: task_completed reason assist_no_new_evidence observed"
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
