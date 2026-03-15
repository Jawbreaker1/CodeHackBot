#!/usr/bin/env bash
set -euo pipefail

SESSIONS_DIR="${SESSIONS_DIR:-sessions}"
WORKER_CMD="${WORKER_CMD:-./birdhackbot}"
APPROVAL_TIMEOUT="${APPROVAL_TIMEOUT:-2m}"
REPEAT="${REPEAT:-5}"
SEED="${SEED:-42}"
STAMP="$(date -u +%Y%m%d-%H%M%S)"
BID="benchmark-s36-full-${STAMP}"

echo "[full-gate] benchmark_id=${BID} repeat=${REPEAT} seed=${SEED}"
./birdhackbot-orchestrator benchmark \
  --sessions-dir "${SESSIONS_DIR}" \
  --scenario-pack docs/runbooks/sprint36-full-gate-scenarios.json \
  --benchmark-id "${BID}" \
  --repeat "${REPEAT}" \
  --seed "${SEED}" \
  --planner auto \
  --approval-timeout "${APPROVAL_TIMEOUT}" \
  --worker-cmd "${WORKER_CMD}" \
  --worker-arg worker

summary="${SESSIONS_DIR}/benchmarks/${BID}/summary.json"
echo "[full-gate] summary=${summary}"
echo "[full-gate] NOTE: full gate is manual/periodic and may take significant runtime."
