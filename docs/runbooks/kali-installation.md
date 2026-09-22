# Kali installation

BirdHackBot's supported deployment platform is **Kali Linux Rolling** on the
assessment host. The runtime assumes a Kali userland and uses its verified
security tooling through the approved worker executor. Keep the application
and its session data inside the authorized assessment VM or host boundary.

## Required baseline

Install the small platform baseline before building the repository:

```sh
sudo apt update
sudo apt install -y git ca-certificates golang-go iproute2 curl wget
```

The repository requires Go 1.24.2 or newer and declares the 1.24.13 toolchain.
If the Kali repository provides an older Go release, install a current Go
toolchain before building. `/bin/sh`, coreutils, and standard POSIX utilities
are part of the Kali base system and are required by the executor and test
fixtures.

Verify the baseline:

```sh
go version
command -v git go ip curl wget /bin/sh
```

`iproute2` supplies the fixed read-only `ip -json` observations used by the
intake flow. `curl` and `wget` are the connected-mode web-fetch path for
advisory and product-documentation sources. In `air_gapped` or `offline` mode
they must not be used for external retrieval; enforce the air gap with the
deployment network boundary as well as the application setting.

## Model access

Choose one model path:

- **Local model:** an OpenAI-compatible local endpoint such as LM Studio. No
  cloud credential is required.
- **ChatGPT subscription:** the Codex CLI is required for sign-in and token
  refresh. Follow [the subscription bridge runbook](subscription-bridge.md);
  the application keeps the inference bridge on loopback and does not use API
  key billing.

The model endpoint is not a Kali package. It must be reachable from the
assessment host and configured with the exact model ID exposed by the provider.

## Optional Kali capability packs

The coordinator can use installed assessment tools, but they are not hard
runtime dependencies. Workers verify each binary before use and record the
tool and version in evidence. A fuller assessment image commonly includes:

```sh
sudo apt install -y nmap metasploit-framework exploitdb john hashcat \
  gobuster ffuf sqlmap hydra aircrack-ng enum4linux smbclient \
  impacket-scripts wireshark
```

Package availability varies by Kali snapshot and image policy. Do not install
or update a tool during an assessment without explicit approval. Burp Suite,
additional wordlists, and customer-specific tools should be provisioned and
licensed according to the assessment image policy.

## Browser assessment tools

For web-application screenshots and browser-flow work, the current host can
use Chromium, Firefox, or CutyCapt when present. They are optional until the
typed browser-artifact capability is enabled:

```sh
sudo apt install -y chromium firefox-esr cutycapt xvfb
```

Playwright is not a current runtime dependency. When the browser-worker slice
is added, install and pin its package and browser versions in the assessment
image rather than downloading them implicitly from a worker task.

## Build and validate

From the repository root:

```sh
go build -buildvcs=false -o birdhackbot ./cmd/birdhackbot
go build -buildvcs=false -o birdhackbot-orchestrator ./cmd/birdhackbot-orchestrator
go build -buildvcs=false -o birdhackbot-web ./cmd/birdhackbot-web
go build -buildvcs=false -o birdhackbot-llm-bridge ./cmd/birdhackbot-llm-bridge
```

For repository validation, also install Python 3 and run:

```sh
sudo apt install -y python3
./scripts/ci.sh
```

The CI helpers use Python's standard library and a Unix PTY; Python is not
needed to run the compiled application. Keep the web server and subscription
bridge bound to loopback until authentication, origin protection, and remote
deployment controls are implemented.
