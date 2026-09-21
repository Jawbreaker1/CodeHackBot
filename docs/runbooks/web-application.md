# Web application runbook

`birdhackbot-web` is the browser adapter for the shared assessment coordinator. It does not contain a second worker engine: the coordinator, approvals, evidence, reports, and model-provider contract are the same ones used by the terminal application.

## Start locally

Build and run it from the checkout:

```sh
go build -buildvcs=false -o birdhackbot-web ./cmd/birdhackbot-web
./birdhackbot-web \
  --llm-base-url http://127.0.0.1:1234/v1 \
  --llm-model qwen/qwen3.8-27b
```

The default listener is `127.0.0.1:8080`. The endpoint must implement the OpenAI-compatible chat completions contract accepted by `internal/llmclient`; the web server never exposes that endpoint to the browser. `--llm-token-file` supplies a local bridge token when the model endpoint requires it. Keep the listener on loopback until authentication, origin/CSRF protection, and deployment controls are implemented.

## Conversation and session workflow

The browser opens a model-led intake conversation. Ask about the harness, describe a security question, or provide partial context. The coordinator can request a bounded, read-only observation of the configured workspace, fixed host identity metadata, or this host's network metadata when that resolves the question; the browser shows the exact request and waits for approval. Directory and host results are rendered as typed summaries with expandable raw evidence. It can also guide an exploratory assessment from an authorized objective, without forcing the operator to know a CIDR before discovery. The coordinator asks focused questions when target boundaries remain genuinely unclear, then returns a proposal for review. Enter a stable customer workspace ID there and explicitly start the assessment. The initial screen does not ask the operator to fill out an assessment form. During an assessment, workers may build a small helper when needed, but source creation, dependency use, execution, and validation remain separate visible approval steps with local evidence.

The console keeps customer workspaces and their sessions in a persistent left navigation rail while the coordinator transcript stays in the center. Runtime annotations between turns are collapsed by default and can be expanded to inspect planning, delegation, proposed actions, approvals, execution transitions, and evidence references. The worker inspector holds each task's phase, goal, current step, plan, action, approval state, context usage, remaining budget, evidence, and findings. Selecting a session restores its live view without opening a separate chat surface; at narrow widths the inspector is available from the `Workers` control instead of disappearing.

Each start request produces a separate assessment session below `sessions/web/<customer>/<session-id>`. Intake conversations are kept separately below `sessions/web/intake/<session-id>`. Both directories contain an atomic `session.json` navigation/transcript record beside the assessment authority and evidence files. Workers show progress and pending approvals in the browser; the operator can approve, deny, answer a coordinator question, ask the live coordinator about progress or discoveries, or stop the assessment.

The server discovers these records on startup. A run that was active when the process stopped is shown as interrupted with a **Resume** action; resuming uses the assessment's saved results and consumed model-call budget. Unknown external effects are never replayed automatically. The left rail keeps draft intake conversations as well as customer sessions, and the selected session is restored in the browser after a reload. The model name in the composer opens a session-scoped picker. If the provider exposes `/models`, those IDs are offered; otherwise enter an exact model ID. Model changes are rejected while a turn or assessment is running and are written to that session's metadata.

The customer view at `/api/v1/customers/<customer-id>` combines all sessions created for that customer in the running server. It includes session status, model-authored draft findings from every plan, and links to each session report. `/api/v1/customers/<customer-id>/report` produces a unified Markdown summary. Findings remain drafts for operator review; a CVE match or model statement is not independent verification.

The preview still has no authentication, origin/CSRF protection, or streaming transport. Keep it on loopback and use only an authorized lab; remote deployment remains out of scope until those controls exist.

## Deterministic check

CI runs the complete web binary against a local deterministic model fixture:

```sh
python3 scripts/check_webapp.py /tmp/birdhackbot-web-ci
```

The check creates and completes two approved sessions for one customer, verifies both individual reports, and verifies the unified customer report. It performs no network or target testing.
