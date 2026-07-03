# BirdHackBot / CodeHackBot

BirdHackBot is an AI-assisted security testing assistant for authorized lab environments.

It is designed for hands-on operators who want an LLM-led workflow without losing control of scope, commands, evidence, or reproducibility. The agent reasons about the task, proposes exact actions, executes through a controlled local runtime, and keeps the session inspectable.

## What It Is

BirdHackBot is currently focused on an interactive worker CLI.

The worker can:

- talk through security-testing tasks
- run exact shell commands after approval or session-level allow
- keep multi-turn session context
- capture command logs and evidence locally
- expose inspectable context packets for debugging
- track structured execution facts and recovery state
- support direct tasks and small semantic plans

The long-term direction is a practical lab assistant for reconnaissance, scanning, controlled exploitation, privilege-escalation work, evidence handling, and OWASP-style reporting inside approved environments.

## Safety First

This project is for authorized security testing only.

Primary intended scope:

- Johan Engwall's closed lab systems
- internal networks where authorization is explicit
- local lab files and fixtures

Not allowed by default:

- third-party testing without written authorization
- denial-of-service testing
- persistence
- real data exfiltration

Operational safety rules live in `AGENTS.md`.

## Why This Project Exists

The project is a rebuild of an earlier implementation that became too complex and brittle.

The current design favors:

- a small, inspectable core loop
- generous context instead of premature compaction
- structured execution truth instead of hidden assumptions
- generic recovery semantics instead of task-specific patches
- local evidence that can be reviewed after the run

The goal is not to hardcode pentest recipes. The goal is to give the LLM enough clean context and runtime feedback to make useful decisions while the system preserves boundaries and evidence.

## Quick Start

Build the worker:

```bash
go build -buildvcs=false -o birdhackbot ./cmd/birdhackbot
```

Run it against a local OpenAI-compatible LLM endpoint:

```bash
./birdhackbot \
  --llm-base-url http://127.0.0.1:1234/v1 \
  --llm-model qwen3.5-27b \
  --allow-all
```

For development and live testing, the expected setup is an isolated lab VM with LM Studio or another local OpenAI-compatible model server.

## Interactive Commands

Inside a worker session, these commands are useful for inspection:

```text
/stats
/packet
/plan
/lastlog
/fulloutput
```

Use `--inspect-context` when diagnosing behavior. It writes context snapshots into the session directory.

## Current State

The active implementation lives in:

- `cmd/`
- `internal/`
- `docs/`
- `scripts/`

The old implementation is preserved under `legacy/` for historical reference only. It is not the current design truth.

The worker loop is the active product surface. The orchestrator exists as an entrypoint, but the current focus is making the worker reliable, inspectable, and useful before rebuilding higher-level orchestration.

## Documentation

- `AGENTS.md`: authorization, scope, and safety directives
- `PROJECT.md`: repository working rules
- `docs/architecture.md`: rebuild architecture and design boundaries
- `TASKS.md`: current executable plan
- `ROADMAP.md`: future phase direction
- `DISCOVERIES.md`: lessons and future-phase notes
- `docs/runbooks/acceptance-gates.md`: live validation expectations

## Development Principles

Before non-trivial behavior changes, the project expects a clear:

- objective
- architecture anchor
- reason the change is not a patch
- validation plan

Avoid:

- scenario-specific guardrails
- brittle output parsing
- hidden fallback planners
- hardcoded demo workflows
- treating `legacy/` as current architecture

## License

No license has been declared yet.
