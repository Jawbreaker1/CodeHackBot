#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'EOF'
Usage:
  ./scripts/show_orchestrator_failures.sh [--run <RUN_ID>] [--sessions-dir <DIR>] [--limit <N>] [--tail <N>] [--no-logs]

Examples:
  ./scripts/show_orchestrator_failures.sh --run run-20260219-141425
  ./scripts/show_orchestrator_failures.sh --run run-20260219-141425 --limit 500
  ./scripts/show_orchestrator_failures.sh   # uses latest run-* in sessions/
EOF
}

RUN_ID=""
SESSIONS_DIR="sessions"
LIMIT="300"
TAIL_LINES="80"
SHOW_LOGS="1"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --run)
      RUN_ID="${2:-}"
      shift 2
      ;;
    --sessions-dir)
      SESSIONS_DIR="${2:-}"
      shift 2
      ;;
    --limit)
      LIMIT="${2:-}"
      shift 2
      ;;
    --tail)
      TAIL_LINES="${2:-}"
      shift 2
      ;;
    --no-logs)
      SHOW_LOGS="0"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ -z "$RUN_ID" ]]; then
  RUN_ID="$(find "$SESSIONS_DIR" -maxdepth 1 -mindepth 1 -type d -name 'run-*' -print 2>/dev/null | awk -F/ '{print $NF}' | sort | tail -n 1 || true)"
fi

if [[ -z "$RUN_ID" ]]; then
  echo "Error: no run id found. Provide --run or ensure $SESSIONS_DIR has run-* folders." >&2
  exit 2
fi

ORCH_BIN="./birdhackbot-orchestrator"
tmp_events="$(mktemp)"
trap 'rm -f "$tmp_events"' EXIT

events_cmd=()
if [[ -x "$ORCH_BIN" ]]; then
  events_cmd=("$ORCH_BIN" events --sessions-dir "$SESSIONS_DIR" --run "$RUN_ID" --limit "$LIMIT")
else
  events_cmd=(go run ./cmd/birdhackbot-orchestrator events --sessions-dir "$SESSIONS_DIR" --run "$RUN_ID" --limit "$LIMIT")
fi

if ! "${events_cmd[@]}" >"$tmp_events"; then
  echo "Failed to read events for run: $RUN_ID" >&2
  exit 1
fi

echo "Run: $RUN_ID"
echo "Sessions dir: $SESSIONS_DIR"
echo

if command -v jq >/dev/null 2>&1; then
  echo "Failure timeline (ts | type | task | worker | reason | error | log_path):"
  jq -r '
    select(.type=="task_failed" or .type=="run_stopped" or .type=="run_replan_requested" or .type=="worker_stopped") |
    [
      .ts,
      .type,
      (.task_id // ""),
      (.worker_id // ""),
      (.payload.reason // ""),
      (.payload.error // ""),
      (.payload.log_path // "")
    ] | @tsv
  ' "$tmp_events"
  echo

  if [[ "$SHOW_LOGS" == "1" ]]; then
    mapfile -t log_paths < <(jq -r 'select(.payload.log_path != null and .payload.log_path != "") | .payload.log_path' "$tmp_events" | awk '!seen[$0]++')
    if [[ "${#log_paths[@]}" -eq 0 ]]; then
      echo "No log_path fields found in failure events."
      exit 0
    fi
    echo "Worker log tails:"
    for log_path in "${log_paths[@]}"; do
      echo
      echo "==> $log_path"
      if [[ -f "$log_path" ]]; then
        tail -n "$TAIL_LINES" "$log_path"
      else
        echo "(missing file)"
      fi
    done
  fi
else
  echo "jq not found; showing raw failure-related events:"
  grep -E '"type":"(task_failed|run_stopped|run_replan_requested|worker_stopped)"' "$tmp_events" || true
fi
