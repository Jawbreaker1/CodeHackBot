# BirdHackBot / CodeHackBot

BirdHackBot is being built as System Verification's security testing platform for authorized assessments. The intended primary product is a multi-agent orchestrator coordinating investigation, source analysis, target validation, and reproducible reporting.

## What works today

The active implementation uses one adaptive worker for standalone and delegated tasks: model-authored plans and revisions, per-action approvals, exact argv or explicit shell execution, whole-goal evaluation, local evidence, bounded context views, and session snapshots. The [worker audit](docs/worker-foundation-audit-2026-09-20.md) records the current rebuild and validation limits.

A local authenticated REST bridge provides subscription-backed OpenAI inference. Launching without flags now opens a guided lab assessment with a coordinator, up to two concurrent workers, serialized action approvals, saved evidence, and a draft report. Source-to-deployment correlation, assessment resume, and independent finding verification remain planned. The runtime does not enforce target allowlists or provide its own network sandbox; execution relies on the isolated lab environment and the operating rules in [AGENTS.md](AGENTS.md).

Current implementation order: **core cleanup → subscription API wrapper → orchestration → source-assisted assessment**. [TASKS.md](TASKS.md) records actual progress.

Required product capabilities include Kali tooling, adaptable playbooks, reusable custom tools, discovery-driven vulnerability research, and fully air-gapped assessments. Local-model access works today; full offline operation and a competitive discovery advantage remain unvalidated. See the [architecture](docs/architecture.md) and [acceptance gates](docs/runbooks/acceptance-gates.md).

Astra is the development/review model. The intended OpenAI pentest runtime is Daybreak on GPT-5.6 Sol, alongside local models. The subscription bridge has called `gpt-daybreak-blue-latest` successfully; the backend reports `gpt-5.6-sol`. Access remains account-dependent.

## Build and run

```sh
go build -buildvcs=false -o birdhackbot ./cmd/birdhackbot
./birdhackbot
```

Run from the repository checkout for this lab preview. Guided setup offers a local model server or an existing Codex ChatGPT sign-in, then asks for a goal and explicit scope before starting. Provider preferences are remembered locally; scope and permissions are reviewed for each assessment. Subscription bridge startup and its temporary local credential are managed by the application. First-time Codex sign-in still uses the [subscription setup guide](docs/runbooks/subscription-bridge.md).

Local setup also saves an explicit reasoning choice. The guided local profile allows 32,768 output tokens and up to ten minutes per request; Ctrl-C cancels an active request. The current lab configuration is Qwen 3.8 27B Q6_K with low reasoning, a server context of 50,176 tokens, and two parallel slots. Server load settings remain managed in LM Studio. The standalone development CLI does not yet expose these guided inference settings.

Each proposed action requires approval. Ctrl-C stops all workers and saves an aborted result. Reports, coordinator decisions, worker state, and evidence live under the displayed `sessions/assessment-*` directory. Reports are model-authored drafts for operator review, not claims of independent vulnerability verification. `birdhackbot-orchestrator` opens the same guided surface.

The standalone worker remains available for development:

```sh
./birdhackbot --llm-base-url http://127.0.0.1:1234/v1 --llm-model YOUR_LOCAL_MODEL_ID
```

Use the exact model ID exposed by your local server. Execution requires per-action approval by default and uses the text interface. Inside the approved isolated VM, explicit `--allow-all` enables session-level approval and the terminal UI.

For a bounded headless task:

```sh
./birdhackbot --goal "Show the current directory"   --llm-base-url http://127.0.0.1:1234/v1 --llm-model YOUR_LOCAL_MODEL_ID   --session-dir sessions/example --max-steps 4 --inspect-context
```

Use a fresh session directory per independent run. `--resume --session-dir PATH` loads a version 2 worker snapshot and preserves its consumed turn budget. Unknown pending execution is never replayed automatically. Older version 1 snapshots remain inspectable files but cannot be resumed with this worker; they lack reliable budget accounting. Whole-assessment resume is not yet implemented.

For subscription access, follow the [bridge setup guide](docs/runbooks/subscription-bridge.md). It uses your Codex ChatGPT sign-in, keeps tool execution in BirdHackBot, and never falls back to API-key billing. Selected worker context is sent to OpenAI.

## Inspect a session

Interactive commands include `/status`, `/plan`, `/stats`, `/packet`, `/lastlog`, and `/fulloutput`. Use `--inspect-context` to save model-facing snapshots.

Each execution records its prepared invocation, working directory, times, exit status, and output references. Stdout/stderr stream to `.stdout` and `.stderr` files alongside the command log. Model previews are bounded; full output stays available locally. Ctrl-C/SIGTERM cancels active work and records an aborted state.

Evidence and session data are ignored by Git and remain local. Testing scope and allowed operations are defined in [AGENTS.md](AGENTS.md); publicly designated test targets have their own [restrictions](docs/roe/public-test-targets.md).

## Development

```sh
./scripts/ci.sh
go test -race ./...
```

The repeat harness accepts an explicit local model endpoint and captures each run in its own session directory. Focused live checks and full product acceptance have different claims; see the [acceptance gates](docs/runbooks/acceptance-gates.md).

Keep changes small and tied to demonstrated failures or agreed requirements. Define done and stop after required validation passes. Avoid speculative frameworks, hidden fallback planners, and scenario-specific runtime fixes.

## Documentation

| Document | Owns |
| --- | --- |
| [AGENTS.md](AGENTS.md) | Authorization and operating rules |
| [PROJECT.md](PROJECT.md) | Repository conventions and implementation discipline |
| [Architecture](docs/architecture.md) | Current contracts and clearly labeled planned boundaries |
| [TASKS.md](TASKS.md) | Immediate sequence and actual implementation status |
| [ROADMAP.md](ROADMAP.md) | Future product milestones |
| [DISCOVERIES.md](DISCOVERIES.md) | Decisions, findings, and validation references |
| [Subscription bridge](docs/runbooks/subscription-bridge.md) | Subscription setup, compatibility contract, and limits |
| [Acceptance gates](docs/runbooks/acceptance-gates.md) | What validation establishes |
| [Baseline assessment](docs/code-assessment-2026-09-19.md) | Historical evidence behind the cleanup |
| [Competitive assessment](docs/competitive-assessment-2026-09-19.md) | Current competitor research, evidence limits, and recommended priorities |

Earlier plans are archived under `docs/archive/`; old code is under `legacy/`. Neither is current design authority. The pre-implementation checkpoint is `checkpoint/pre-core-rebuild-2026-09-19` (`95edae1`).
