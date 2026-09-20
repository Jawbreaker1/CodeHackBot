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

The browser opens a model-led intake conversation. Ask about the harness, describe a security question, or provide partial context; the coordinator asks for missing objective, exact targets, allowed actions, and exclusions. It returns a proposal only when it has enough information. The browser then shows a compact review card. Enter a stable customer workspace ID there and explicitly start the assessment. The initial screen does not ask the operator to fill out an assessment form.

The console keeps customer workspaces and their sessions in a persistent left navigation rail while the coordinator transcript stays in the center. The right inspector holds the current scope, model calls, approvals, activity, results, and report links. Selecting a session restores its live view without opening a separate chat surface.

Each start request produces a separate assessment session below `sessions/<customer>/<session-id>`. Workers show progress and pending approvals in the browser; the operator can approve, deny, answer a coordinator question, ask the live coordinator about progress or discoveries, or stop the assessment.

The customer view at `/api/v1/customers/<customer-id>` combines all sessions created for that customer in the running server. It includes session status, model-authored draft findings from every plan, and links to each session report. `/api/v1/customers/<customer-id>/report` produces a unified Markdown summary. Findings remain drafts for operator review; a CVE match or model statement is not independent verification.

The current preview keeps the server's session index in memory. Restarting the process does not rediscover previous sessions, and the preview has no authentication. Durable discovery/resume, streaming events, and remote deployment are follow-up work.

## Deterministic check

CI runs the complete web binary against a local deterministic model fixture:

```sh
python3 scripts/check_webapp.py /tmp/birdhackbot-web-ci
```

The check creates and completes two approved sessions for one customer, verifies both individual reports, and verifies the unified customer report. It performs no network or target testing.
