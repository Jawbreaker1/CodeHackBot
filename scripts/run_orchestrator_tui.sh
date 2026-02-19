#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'EOF'
Usage:
  ./scripts/run_orchestrator_tui.sh [options]

Options:
  --goal TEXT                Goal text for orchestrator planning.
  --scope-network CIDR       In-scope network (repeatable).
  --scope-target TARGET      In-scope host/target (repeatable).
  --constraint TEXT          Run constraint (repeatable).
  --run-id ID                Explicit run id (default: run-YYYYmmdd-HHMMSS).
  --sessions-dir DIR         Sessions directory (default: sessions).
  --max-parallelism N        Max parallel workers (default: 2).
  --permissions MODE         readonly|default|all (default: default).
  --planner MODE             static|llm|auto (default: auto).
  --worker-cmd PATH          Worker binary path (default: ./birdhackbot).
  --worker-arg ARG           Worker arg (repeatable, default: worker).
  --llm-base-url URL         Export BIRDHACKBOT_LLM_BASE_URL for this run.
  --llm-model MODEL          Export BIRDHACKBOT_LLM_MODEL for this run.
  --llm-timeout SECONDS      Export BIRDHACKBOT_LLM_TIMEOUT_SECONDS for this run.
  --skip-build               Skip local go build step.
  -h, --help                 Show this help.

Examples:
  ./scripts/run_orchestrator_tui.sh \
    --goal "Map services in 192.168.50.0/24 and summarize findings" \
    --scope-network 192.168.50.0/24 \
    --constraint internal_lab_only

  ./scripts/run_orchestrator_tui.sh \
    --goal "Assess web target and report findings" \
    --scope-target 192.168.50.78 \
    --constraint internal_lab_only \
    --llm-model qwen/qwen3-coder-30b
EOF
}

RUN_ID="run-$(date +%Y%m%d-%H%M%S)"
SESSIONS_DIR="sessions"
GOAL=""
MAX_PARALLELISM="2"
PERMISSIONS="default"
PLANNER_MODE="auto"
WORKER_CMD="$ROOT_DIR/birdhackbot"
SKIP_BUILD=0

SCOPE_NETWORKS=()
SCOPE_TARGETS=()
CONSTRAINTS=()
WORKER_ARGS=("worker")

while [[ $# -gt 0 ]]; do
  case "$1" in
    --goal)
      GOAL="${2:-}"
      shift 2
      ;;
    --scope-network)
      SCOPE_NETWORKS+=("${2:-}")
      shift 2
      ;;
    --scope-target)
      SCOPE_TARGETS+=("${2:-}")
      shift 2
      ;;
    --constraint)
      CONSTRAINTS+=("${2:-}")
      shift 2
      ;;
    --run-id)
      RUN_ID="${2:-}"
      shift 2
      ;;
    --sessions-dir)
      SESSIONS_DIR="${2:-}"
      shift 2
      ;;
    --max-parallelism)
      MAX_PARALLELISM="${2:-}"
      shift 2
      ;;
    --permissions)
      PERMISSIONS="${2:-}"
      shift 2
      ;;
    --planner)
      PLANNER_MODE="${2:-}"
      shift 2
      ;;
    --worker-cmd)
      WORKER_CMD="${2:-}"
      shift 2
      ;;
    --worker-arg)
      WORKER_ARGS+=("${2:-}")
      shift 2
      ;;
    --llm-base-url)
      export BIRDHACKBOT_LLM_BASE_URL="${2:-}"
      shift 2
      ;;
    --llm-model)
      export BIRDHACKBOT_LLM_MODEL="${2:-}"
      shift 2
      ;;
    --llm-timeout)
      export BIRDHACKBOT_LLM_TIMEOUT_SECONDS="${2:-}"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD=1
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

if [[ -z "$GOAL" ]]; then
  read -r -p "Goal: " GOAL
fi
if [[ "${#SCOPE_NETWORKS[@]}" -eq 0 && "${#SCOPE_TARGETS[@]}" -eq 0 ]]; then
  read -r -p "Scope network (CIDR) or target host/IP: " scope_input
  if [[ "$scope_input" == */* ]]; then
    SCOPE_NETWORKS+=("$scope_input")
  elif [[ -n "$scope_input" ]]; then
    SCOPE_TARGETS+=("$scope_input")
  fi
fi
if [[ "${#CONSTRAINTS[@]}" -eq 0 ]]; then
  CONSTRAINTS+=("internal_lab_only")
fi

if [[ -z "$GOAL" ]]; then
  echo "Error: goal is required." >&2
  exit 2
fi
if [[ "${#SCOPE_NETWORKS[@]}" -eq 0 && "${#SCOPE_TARGETS[@]}" -eq 0 ]]; then
  echo "Error: at least one --scope-network or --scope-target is required." >&2
  exit 2
fi

if [[ "$SKIP_BUILD" -ne 1 ]]; then
  echo "[build] go build ./cmd/birdhackbot"
  go build -o birdhackbot ./cmd/birdhackbot
  echo "[build] go build ./cmd/birdhackbot-orchestrator"
  go build -o birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator
fi

cmd=(
  ./birdhackbot-orchestrator run
  --sessions-dir "$SESSIONS_DIR"
  --run "$RUN_ID"
  --goal "$GOAL"
  --max-parallelism "$MAX_PARALLELISM"
  --planner "$PLANNER_MODE"
  --worker-cmd "$WORKER_CMD"
  --permissions "$PERMISSIONS"
  --tui
)

for c in "${CONSTRAINTS[@]}"; do
  [[ -n "$c" ]] && cmd+=(--constraint "$c")
done
for n in "${SCOPE_NETWORKS[@]}"; do
  [[ -n "$n" ]] && cmd+=(--scope-network "$n")
done
for t in "${SCOPE_TARGETS[@]}"; do
  [[ -n "$t" ]] && cmd+=(--scope-target "$t")
done
for a in "${WORKER_ARGS[@]}"; do
  [[ -n "$a" ]] && cmd+=(--worker-arg "$a")
done

echo "[run] RUN_ID=$RUN_ID"
echo "[run] sessions-dir=$SESSIONS_DIR"
echo "[run] goal=$GOAL"
echo "[run] planner=$PLANNER_MODE"
if [[ -n "${BIRDHACKBOT_LLM_BASE_URL:-}" ]]; then
  echo "[run] llm-base-url=$BIRDHACKBOT_LLM_BASE_URL"
fi
if [[ -n "${BIRDHACKBOT_LLM_MODEL:-}" ]]; then
  echo "[run] llm-model=$BIRDHACKBOT_LLM_MODEL"
fi

"${cmd[@]}"
exit_code=$?
echo
echo "[debug] Inspect failures:"
echo "./scripts/show_orchestrator_failures.sh --run $RUN_ID"
exit "$exit_code"
