# Kali Operator Runbook (Orchestrator + Worker Mode)

This runbook is for authorized internal lab usage only.

## 1) Prerequisites
- Kali Linux VM (internal lab network only)
- Go 1.22+ (`go version`)
- Tools in PATH as needed by plan tasks (`nmap`, `curl`, `python3`, etc.)
- Local LLM endpoint configured for worker-side tasks that require LLM access

## 2) Build
```bash
go build -buildvcs=false -o birdhackbot ./cmd/birdhackbot
go build -buildvcs=false -o birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator
```

## 3) Prepare a run plan
Create `plan.json` with required fields (`scope`, `constraints`, `success_criteria`, `stop_criteria`, `tasks`).

Minimal task action example:
```json
{
  "task_id": "t1",
  "goal": "Collect target headers",
  "targets": ["192.168.50.10"],
  "done_when": ["headers captured"],
  "fail_when": ["timeout"],
  "expected_artifacts": ["command log"],
  "risk_level": "recon_readonly",
  "action": {
    "type": "command",
    "command": "curl",
    "args": ["-sv", "http://192.168.50.10"]
  },
  "budget": { "max_steps": 2, "max_tool_calls": 2, "max_runtime": 30000000000 }
}
```

## 4) Start and run
```bash
./birdhackbot-orchestrator start --sessions-dir sessions --plan plan.json --run lab-run-001

./birdhackbot-orchestrator run \
  --sessions-dir sessions \
  --run lab-run-001 \
  --worker-cmd ./birdhackbot \
  --worker-arg worker \
  --permissions default \
  --tick 250ms
```

Notes:
- `--permissions readonly|default|all` controls risk handling.
- `--disruptive-opt-in` is required before disruptive actions are even approvable.

## 5) Monitor and approvals
```bash
./birdhackbot-orchestrator status --sessions-dir sessions --run lab-run-001
./birdhackbot-orchestrator workers --sessions-dir sessions --run lab-run-001
./birdhackbot-orchestrator events --sessions-dir sessions --run lab-run-001 --limit 30
./birdhackbot-orchestrator approvals --sessions-dir sessions --run lab-run-001
```

Approve/deny examples:
```bash
./birdhackbot-orchestrator approve --sessions-dir sessions --run lab-run-001 --approval <id> --scope once --actor johan --reason "lab approved"
./birdhackbot-orchestrator deny --sessions-dir sessions --run lab-run-001 --approval <id> --actor johan --reason "out of scope"
```

## 6) Stop and report
```bash
./birdhackbot-orchestrator worker-stop --sessions-dir sessions --run lab-run-001 --worker <worker-id> --actor johan --reason "manual abort"
./birdhackbot-orchestrator stop --sessions-dir sessions --run lab-run-001
./birdhackbot-orchestrator report --sessions-dir sessions --run lab-run-001 --out sessions/lab-run-001/report.md
```

## 7) Troubleshooting
- Worker immediately fails with `scope_denied`:
  - target/action is outside `plan.scope`; fix scope or task targets.
- Worker fails with `policy_denied`:
  - risk tier is blocked by current permission mode.
- Repeated `awaiting_approval` or blocked tasks:
  - inspect `approvals` queue and grant/deny explicitly.
- Stale or reclaimed tasks:
  - check `events` for `worker_recovery`, `startup_sla_missed`, `stale_lease`.
- No progress:
  - verify `workers` output, then inspect latest `task_artifact` log path in events.

## 8) Safety baseline
- Keep scope strictly internal (`scope.networks`/`scope.targets`).
- Do not enable disruptive testing without explicit session opt-in and documented approval.
- Keep all evidence inside lab storage paths under `sessions/`.
