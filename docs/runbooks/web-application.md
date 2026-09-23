# Web application runbook

`birdhackbot-web` is the browser adapter for the shared assessment coordinator. It does not contain a second worker engine: the coordinator, approvals, evidence, reports, and model-provider contract are the same ones used by the terminal application.

## Start locally

Use a Kali Linux Rolling host with the baseline from the [Kali installation
runbook](kali-installation.md). The web binary itself is Go-only; the model
endpoint and any assessment tools are separate provider/image dependencies.

Build and run it from the checkout:

```sh
go build -buildvcs=false -o birdhackbot-web ./cmd/birdhackbot-web
./birdhackbot-web \
  --llm-base-url http://127.0.0.1:1234/v1 \
  --llm-model qwen/qwen3.8-27b
```

The default listener is `127.0.0.1:8080`. The endpoint must implement the OpenAI-compatible chat completions contract accepted by `internal/llmclient`; image-capable local endpoints should accept `image_url` content parts. PDF handling is provider-specific; the subscription bridge maps it to a Responses file input. The web server never exposes that endpoint to the browser. `--llm-token-file` supplies a local bridge token when the model endpoint requires it. Keep the listener on loopback until authentication, origin/CSRF protection, and deployment controls are implemented.

## Conversation and session workflow

The browser opens a model-led intake conversation. The composer can attach up to four bounded PNG, JPEG, WebP, GIF, or PDF artifacts. Attachments are stored locally under the session with restrictive permissions, shown in the transcript, and sent as typed visual/file input only for the current model turn; do not attach credentials or unrelated customer data. The subscription bridge forwards images and PDFs to the Responses model path. Local endpoints must support the corresponding multimodal format; otherwise the coordinator reports the provider limitation. Ask about the harness, describe a security question, or provide partial context. The coordinator can request a bounded, read-only observation of the configured workspace, fixed host identity metadata, or this host's network metadata when that resolves the question; the browser shows the exact request and waits for approval. Directory and host results are rendered as typed summaries with expandable raw evidence. It can also guide an exploratory assessment from an authorized objective, without forcing the operator to know a CIDR before discovery. The coordinator asks focused questions when target boundaries remain genuinely unclear, then returns a proposal for review. Enter a stable customer workspace ID there and explicitly start the assessment. The initial screen does not ask the operator to fill out an assessment form. During an assessment, workers may build a small helper when needed, but source creation, dependency use, execution, and validation remain separate visible approval steps with local evidence.

For an explicitly scoped web target, a delegated worker may use the pinned
Playwright helper in `tools/playwright-runner`. The scenario can traverse
JavaScript routes and role flows and write screenshots, traces, DOM snapshots,
or network captures beneath its task workspace. The worker declares those
paths in its action response; only existing regular files inside that workspace
are registered as evidence. Browser authentication state and captures are
sensitive local evidence and require the same scope and approval discipline as
any other tool action.

The console keeps customer workspaces and their sessions in a persistent left navigation rail while the coordinator transcript stays in the center. Runtime annotations between turns are collapsed by default and can be expanded to inspect planning, delegation, proposed actions, approvals, execution transitions, and evidence references. The right inspector separates each coordinator round's planned work from its observed result. A worker marked *Finished* has ended its assigned task; that label alone never means that the assessment goal was met. The round outcome instead distinguishes work still awaiting review, an unconfirmed goal, a possible finding, and an independently verified finding. The coordinator supplies short operator-facing purpose and review sentences for new rounds; full technical plans and worker reports remain expandable. The inspector also shows each worker's current step, purpose, progress, plan revisions, action, approval state, context usage, remaining budget, evidence, and findings. Plan review and execution approvals still appear in the main conversation. Selecting a session restores its live view without opening a separate chat surface; at narrow widths the inspector is available from the `Workers` control instead of disappearing.

Each start request produces a separate assessment session below `sessions/web/<customer>/<session-id>`. Intake conversations are kept separately below `sessions/web/intake/<session-id>`. Both directories contain an atomic `session.json` navigation/transcript record beside the assessment authority and evidence files. Workers show progress and pending approvals in the browser; the operator can approve, deny, answer a coordinator question, ask the live coordinator about progress or discoveries, or stop the assessment.

The server discovers these records on startup. A run that was active when the process stopped is shown as interrupted with a **Resume** action; resuming uses the assessment's saved results and consumed model-call budget. Unknown external effects are never replayed automatically. The left rail keeps draft intake conversations as well as customer sessions, and the selected session is restored in the browser after a reload. Each row has an explicit delete action protected by a confirmation prompt; deleting removes the session transcript, evidence, report, and linked intake record from the local session store. A running assessment must be stopped and finalized before it can be deleted. The model name in the composer opens a session-scoped picker. If the provider exposes `/models`, those IDs are offered; otherwise enter an exact model ID. Model changes are rejected while a turn or assessment is running and are written to that session's metadata.

The customer view at `/api/v1/customers/<customer-id>` combines all sessions created for that customer in the running server. It includes session status, model-authored draft findings from every plan, and links to each session report. `/api/v1/customers/<customer-id>/report` produces a unified Markdown summary. Findings remain drafts for operator review; a CVE match or model statement is not independent verification.

After a session starts, the coordinator's first non-empty plan is shown in the main conversation. It may be a visible research round when strategy, software identity, or advisory knowledge is missing; this is an operator-selectable worker plan, not a silent tool phase. Select the bounded tasks to run or reject the plan; skipped tasks are retained in the report as operator decisions. The coordinator can return to research after new discoveries. Each selected task follows the selected session approval policy. The coordinator and workers receive the versioned [local strategy catalog](../strategies/catalog.md). The coordinator can suggest up to two guides per task; the right-side plan shows them under **Suggested guidance**. A worker may load a suggested or different guide with `load_strategy`; only the selected guide and its provenance remain in that worker's context through later decisions. CVE and advisory references, observed software, validation status, and evidence links are shown in the analysis workspace at `/analysis?assessment=<session-id>`. Use `/analysis?customer=<customer-id>` for the unified customer view. The analysis surface is separate from chat so priorities, gaps, evidence, remediation, and next actions remain visible while the coordinator conversation continues.

The formal Markdown report is generated from the same persisted assessment state and includes scope, the selected test sequence, findings, advisory references, reproduction steps, remediation, evidence references, work logs, and stated gaps. It is a reviewable draft rather than an automatic assurance document; preserve the local session directory when a customer needs the full execution record.

Set `BIRDHACKBOT_RESEARCH_MODE=air_gapped` before startup to make the product frame prohibit external advisory/documentation fetches and require local snapshots. Connected mode permits workers to use approved `curl`/`wget` actions for advisory/documentation URLs, with URL, status, retrieval time, and saved response recorded in evidence. This setting is a prompt contract; enforce the actual air gap with the deployment network boundary.

The preview still has no authentication, origin/CSRF protection, or streaming transport. Keep it on loopback and use only an authorized lab; remote deployment remains out of scope until those controls exist.

## Deterministic check

CI runs the complete web binary against a local deterministic model fixture:

```sh
python3 scripts/check_webapp.py /tmp/birdhackbot-web-ci
```

The check creates and completes two approved sessions for one customer, verifies both individual reports, and verifies the unified customer report. It performs no network or target testing.

### Approval settings and watching workers

Click the approval label below the composer to choose **Approve every execution**,
**Approve dangerous executions**, or **Approve everything** for that session.
Automatic modes require explicit acknowledgement. Dangerous-only mode relies
on the model's structured risk assessment and still asks for uncertain or
incomplete assessments. Full access skips execution prompts inside the authorized
VM; it does not change scope or prohibited actions. Existing pending requests
still need an explicit decision. New sessions start with approval for every
execution. In the terminal, use `/permissions` for the same choices.

Approval cards show the action's purpose, target and expected impact first.
Expand **Command details** to inspect the exact invocation. While a worker runs,
choose **Watch execution** to see live tool output and its declared browser
preview. **Stop all workers** remains available in that view. The Playwright
helper's named `step` calls describe what the scenario is doing, and its trace
preserves the detailed interaction history locally.
