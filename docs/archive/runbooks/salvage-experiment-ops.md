# Salvage Experiment Ops (Sprint 36)

Date: 2026-02-26  
Scope: authorized internal lab only

This runbook standardizes how we execute and record Sprint 36 salvage experiments so runs are reproducible and cheap to iterate.

## 0) Sprint Freeze Policy

During Sprint 36:
- do not start new feature work from Sprint 37+ tracks.
- only allow changes that map to active Sprint 36 salvage phases.
- if a non-salvage change is discovered as necessary, first record the dependency in `docs/runbooks/problem-register.md`, then proceed as a salvage prerequisite.

## 1) Run-ID Standard

Use explicit run IDs for all manual salvage runs.

Format:
- `salvage-<problem-id>-<slice>-<YYYYMMDD-HHMMSS>-s<seed>`

Examples:
- `salvage-pr001-loopguard-20260226-181500-s11`
- `salvage-pr002-plannerretry-20260226-182020-s17`

Rules:
- `problem-id` maps to `docs/runbooks/problem-register.md` (`pr001`, `pr002`, ...).
- `slice` is short and implementation-focused (`diagmode`, `ctxcarry`, `contractgate`).
- include seed in run ID when seed is fixed.
- benchmark IDs follow the same pattern:
  - `benchmark-salvage-<problem-id>-<YYYYMMDD-HHMMSS>`

## 2) Required Experiment Metadata

Record these fields in the problem-register checkpoint log:
- problem ID
- change slice summary
- exact command(s)
- planner mode (`auto|llm|static`)
- seed(s)
- pass/fail result
- dominant failure reason (if failed)

## 3) Artifact Checklist (Per Run)

Every salvage run must provide:
- run events:
  - `sessions/<run-id>/orchestrator/event/event.jsonl`
- plan snapshot:
  - `sessions/<run-id>/orchestrator/plan/plan.json`
- task leases:
  - `sessions/<run-id>/orchestrator/task/leases.json`
- report:
  - `sessions/<run-id>/orchestrator/report.md`

For assist-heavy tasks also require:
- context diagnostics:
  - `sessions/<run-id>/orchestrator/artifact/<task-id>/context_envelope.json`
- when tracing is enabled:
  - `sessions/<run-id>/orchestrator/artifact/<task-id>/*-llm-response.json`

For planner failures also require:
- planner attempts:
  - `sessions/planner-attempts/<run-id>/attempts.json`

## 4) Quota-Aware Validation Cadence

Use this cadence by default:

1. Per code change:
- run targeted unit tests for touched areas.
- run one focused smoke scenario (`repeat=1`, one seed).
- use `--diagnostic` only when root-cause tracing is needed.

2. Per milestone slice (checkpoint-worthy):
- run quick gate with at least 2 seeds and `--planner auto`.
- confirm no repeated low-value churn signatures (`no_progress` preferred over silent completion).
- log results in `docs/runbooks/problem-register.md`.

3. Periodic/manual only:
- full benchmark suite (`repeat=5`) and baseline refresh.
- do not run per commit unless a release/merge gate requires it.

## 5) Command Templates

Single-run smoke:

```bash
rid="salvage-pr001-loopguard-$(date -u +%Y%m%d-%H%M%S)-s11"
./birdhackbot-orchestrator run \
  --sessions-dir sessions \
  --run "$rid" \
  --goal "Extract contents of secret.zip and identify the password needed to decrypt it. Produce a concise local report with method, result, and evidence paths." \
  --scope-local \
  --constraint local_only \
  --planner auto \
  --approval-timeout 2m \
  --worker-cmd ./birdhackbot \
  --worker-arg worker
```

Diagnostic trace run:

```bash
rid="salvage-pr001-diagmode-$(date -u +%Y%m%d-%H%M%S)-s11"
./birdhackbot-orchestrator run \
  --sessions-dir sessions \
  --run "$rid" \
  --goal "Extract contents of secret.zip and identify the password needed to decrypt it. Produce a concise local report with method, result, and evidence paths." \
  --scope-local \
  --constraint local_only \
  --planner auto \
  --diagnostic \
  --approval-timeout 2m \
  --worker-cmd ./birdhackbot \
  --worker-arg worker
```

Quick benchmark slice:

```bash
bid="benchmark-salvage-pr001-$(date -u +%Y%m%d-%H%M%S)"
./birdhackbot-orchestrator benchmark \
  --sessions-dir sessions \
  --benchmark-id "$bid" \
  --scenario recovery_stress_repeat_pressure \
  --repeat 1 \
  --seed 11 \
  --planner auto \
  --worker-cmd ./birdhackbot \
  --worker-arg worker \
  --approval-timeout 2m
```
