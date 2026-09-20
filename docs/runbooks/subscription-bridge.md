# Subscription inference bridge

Implemented 2026-09-19. This is a local text-inference adapter for the current worker, not a general OpenAI API replacement.

## Approach and references

We checked existing tools before choosing the integration:

| Tool | Verified approach | What BirdHackBot uses |
| --- | --- | --- |
| [OpenCode provider](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/plugin/openai/codex.ts) | OAuth access/refresh tokens, account header, direct Codex Responses requests | The same inference boundary; BirdHackBot keeps its own worker and executor |
| [Cline subscription support](https://cline.bot/blog/introducing-openai-codex-oauth) | Subscription sign-in with managed token refresh | Conceptual confirmation that subscription access can be a provider choice |
| [Codex authentication](https://learn.chatgpt.com/docs/auth) and [account interface](https://learn.chatgpt.com/docs/app-server) | Existing ChatGPT login, file credentials, account refresh | Reuse sign-in and refresh instead of implementing another OAuth credential store in this first slice |

No third-party implementation code or documentation was copied. OpenCode was inspected for protocol compatibility; Cline was used only as conceptual reference.

Inference goes directly to `https://chatgpt.com/backend-api/codex/responses`. This is a Codex backend compatibility integration, not a documented stable public inference API. Backend changes may require adapter updates. Codex CLI is needed for sign-in and expired-token refresh; no Codex model turn or agent is launched.

## Setup

For normal use, build `birdhackbot`, run it without flags from the checkout, and select **ChatGPT subscription**. With an existing file-based Codex sign-in, the application starts and stops its own loopback bridge and removes its temporary client credential on exit. No separate bridge command or manual local token is needed. First sign-in still uses Codex as described below. The remaining commands document standalone bridge/development use.

Build from the repository root:

```sh
go build -buildvcs=false -o birdhackbot ./cmd/birdhackbot
go build -buildvcs=false -o birdhackbot-llm-bridge ./cmd/birdhackbot-llm-bridge
```

Use a ChatGPT subscription login in Codex CLI. An existing file-based ChatGPT login works directly. If you need to sign in, run:

```sh
codex -c 'cli_auth_credentials_store="file"' login
```

The first implementation requires `auth.json` under `CODEX_HOME` (default `~/.codex`); keychain-only credential storage is not supported. `--codex-home PATH` selects a different directory. Use the same `CODEX_HOME` when signing in. Codex CLI `0.154.0` was used for local validation.

Create a separate local client token outside the repository and start the bridge:

```sh
mkdir -p "$HOME/.config/birdhackbot"
chmod 700 "$HOME/.config/birdhackbot"
./birdhackbot-llm-bridge --token-file "$HOME/.config/birdhackbot/bridge-token" --init-token
./birdhackbot-llm-bridge --token-file "$HOME/.config/birdhackbot/bridge-token"
```

Run `--init-token` once; it refuses to overwrite an existing token. It creates a random 256-bit token with file mode `0600` and never prints its value.

In another terminal:

```sh
./birdhackbot --llm-base-url http://127.0.0.1:8787/v1 \
  --llm-model gpt-daybreak-blue-latest \
  --llm-token-file "$HOME/.config/birdhackbot/bridge-token"
```

Execution still requires the worker's normal operator approvals. For resumed sessions, supply `--llm-token-file` again; neither its path nor its contents is persisted in session state. The repeat harness accepts `--token-file PATH`.

## Contract and limits

- Only authenticated `POST /v1/chat/completions` is exposed. Input is a model ID and text messages with system, developer, user, or assistant roles. Unsupported fields, tools, and client streaming requests are rejected.
- `temperature` is accepted for the existing worker wire format but omitted upstream. Model defaults apply. Images, reasoning controls, function-call forwarding, model discovery, remote hosting, and scheduling are deferred.
- Upstream requests use `store: false`, `tools: []`, and `tool_choice: "none"`. Responses streams are collected into one Chat Completions response, including token usage. Only a completed response with usable text succeeds; unfinished streams and unexpected tool output fail.
- The requested model ID is sent unchanged. The response preserves the backend's resolved model name. No automatic substitution occurs. Access depends on the signed-in account, not a hardcoded supported-model list.
- The bridge uses only subscription credentials. API-key credentials are rejected; environment API keys are not used. Subscription limits and any account credit settings still apply. This does not promise unlimited usage or free access.
- A `401` causes one Codex-managed refresh and one retry. Further auth failure requires signing in again. `403` reports access denial, `429` reports the account limit and preserves `Retry-After` when supplied, and failed/incomplete streams return `502`. No model or paid-API fallback occurs.
- Canceling the client request cancels upstream inference. SIGINT/SIGTERM cancels active bridge requests. The existing worker client timeout is 90 seconds; the bridge has a three-minute request ceiling.

The listener accepts only literal loopback IPs and local clients with the separate bearer token. Browser-origin requests and redirects are rejected. Credentials are excluded from model context, session artifacts, process arguments, and tool environments. Filesystem access by other processes under the same OS user is not prevented by this arrangement: use the assessment VM's isolation controls. The worker has no independent target/network sandbox yet.

Cloud inference sends the selected worker context, including tool evidence in that context, to OpenAI. Raw local evidence remains in the workspace. `store: false` is a request option, not a claim about all provider data retention.

## Validation

The signed-in Codex catalog exposed `gpt-daybreak-blue-latest`; a real request using that alias reported resolved model `gpt-5.6-sol`. This verifies this account's route, not universal availability or a separate audit of provider-side safety settings.

The first live transport probe found that the terminal event could omit output already delivered through completed-item events. The parser was corrected and this exact behavior has a regression test. Mock tests cover the client-to-bridge-to-provider path, auth rejection/refresh, rate/access errors, redirects, cancellation, and incomplete/tool output. Expired credentials and exhausted quotas are simulated; the account was not deliberately expired or exhausted.

Worker validation and evidence are recorded in [acceptance gates](acceptance-gates.md) and [discoveries](../../DISCOVERIES.md). These checks establish model access and execution ownership, not pentest effectiveness or readiness for customer assessments.
