# BirdHackBot

BirdHackBot is an AI-assisted security testing CLI built to make professional-style security validation easier, faster, and more repeatable.

## Why BirdHackBot Exists

Most security testing tools are powerful, but they require deep expertise and a lot of manual coordination. BirdHackBot acts like an AI testing partner: it helps plan the work, run the right checks, adapt when something fails, and explain what matters in plain language.

The goal is simple: help teams find real security weaknesses earlier, with clear evidence and useful reporting.

## Core Purpose

- Turn exploratory security testing into a guided, step-by-step workflow.
- Help users move from “what should we test?” to “here is what we found” faster.
- Keep testing safer with scope controls, approvals, and interrupt handling.
- Produce outputs that are understandable for both engineers and decision-makers.

## Core Features

- Agentic CLI workflow: the assistant can plan, execute, recover, and continue toward a goal.
- Dynamic context and memory: keeps track of prior actions, evidence, and conversation during long sessions.
- Permission and safety modes: `readonly`, `default` (approval required), and `all` for sandboxed lab use.
- On-demand tool creation: can generate, run, and refine Python/Bash helper tools as needed for the current task.
- Built-in execution tools: local command execution, file primitives, browsing/crawling helpers, and reusable per-session tool artifacts.
- Metasploit-aware guidance: can query Metasploit modules during a session to identify relevant vulnerability/exploit paths on the fly.
- Evidence-first logging: session artifacts, command logs, and reproducible outputs.
- Report support: structured findings and security reporting workflows.

## Orchestration Layer

BirdHackBot includes a second layer for larger campaigns: `birdhackbot-orchestrator`.

- Control multiple worker runs from one place (including parallel task execution).
- Keep operations safer with explicit task leases, retries, approvals, and bounded budgets.
- Preserve stronger operational visibility through a live TUI (tasks, workers, approvals, failures, events).
- Merge artifacts/findings into one run-level view for cleaner handoff and reporting.
- Run repeatable autonomy benchmarks with per-run scorecards and baseline locking.

In short: the CLI is your hands-on AI tester, while the orchestrator is the control tower for repeatable multi-step execution.

### Orchestrator Roles (Current Model)

- `Operator` (you): defines goals, approves riskier actions, and steers runs through TUI/CLI (`ask`, `instruct`, approvals).
- `Planner`: turns goals + constraints into structured task plans (static or LLM-backed).
- `Coordinator/Manager`: enforces contracts, scheduling, scope limits, retries/replans, and run state transitions.
- `Workers`: execute scoped tasks, emit evidence/findings/events, and remain isolated per worker workspace.
- `Approval Broker`: centralizes policy/risk-tier decisions so approvals are consistent and auditable.
- `Reporting Layer`: merges evidence/findings into run-level reports and benchmark scorecards.

This role split is the main value-add: stronger control, safer autonomy, and more reproducible outcomes than a single-agent loop.

### Quick Architecture View

```text
Operator (TUI/CLI)
        |
        v
Planner -----> Coordinator/Manager -----> Approval Broker
                       |
                       v
                Worker Pool (N)
                       |
                       v
            Evidence + Findings + Events
                       |
                       v
          Reporting + Benchmark Scorecards
```

## Who It’s For

BirdHackBot is designed for authorized security testing in controlled environments — from hands-on testers to technical leads who need clear, credible security insights without stitching together dozens of disconnected tools.
