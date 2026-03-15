#!/usr/bin/env bash
set -euo pipefail

SESSIONS_DIR="${SESSIONS_DIR:-sessions}"
WORKER_CMD="${WORKER_CMD:-./birdhackbot}"
APPROVAL_TIMEOUT="${APPROVAL_TIMEOUT:-2m}"
STAMP="$(date -u +%Y%m%d-%H%M%S)"

run_case() {
  local scenario="$1"
  local seed="$2"
  local bid="benchmark-s36-quick-${scenario}-${STAMP}-s${seed}"
  echo "[quick-gate] scenario=${scenario} seed=${seed} benchmark_id=${bid}"
  ./birdhackbot-orchestrator benchmark \
    --sessions-dir "${SESSIONS_DIR}" \
    --scenario-pack docs/runbooks/sprint36-quick-gate-scenarios.json \
    --scenario "${scenario}" \
    --benchmark-id "${bid}" \
    --repeat 1 \
    --seed "${seed}" \
    --planner auto \
    --approval-timeout "${APPROVAL_TIMEOUT}" \
    --worker-cmd "${WORKER_CMD}" \
    --worker-arg worker

  local summary="${SESSIONS_DIR}/benchmarks/${bid}/summary.json"
  python3 scripts/check_benchmark_gate.py \
    --summary "${summary}" \
    --sessions-dir "${SESSIONS_DIR}" \
    --max-low-value-streak 3
}

run_case "recovery_stress_repeat_pressure" "11"
run_case "evidence_first_reporting_quality" "17"

echo "[quick-gate] complete"
