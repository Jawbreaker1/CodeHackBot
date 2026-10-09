# Held-out source review through the browser

On 2026-10-04, a new local Python service fixture was created outside the BirdHackBot repository at `/tmp/bhb-review-20261004` and pinned to Git revision `563a411458d3ddcd8ca6ae5c2351eb1bca9f7f07`. The assessment prompt named the source path, allowed bounded synthetic tests, and explained that its `Principal` object comes from trusted middleware. It did not disclose the planted weaknesses or give exploitation steps. This was a fresh GUI run using `gpt-daybreak-blue-latest` with the default per-execution approval level.

The coordinator assigned two parallel source reviews. One worker found both material defects; the second independently found the path defect. The coordinator then assigned separate verification workers. Both reproduced their assigned issue with fresh synthetic data and negative controls. The final analysis and Markdown report identify:

- **High, reproduced:** the claim-note export checks a URL-controlled workspace instead of the authenticated principal's workspace, allowing cross-workspace note disclosure.
- **High, reproduced:** evidence-file containment uses a string-prefix comparison on resolved paths, allowing a prefix-matching sibling file to be read.
- **Low, candidate:** requesting a directory propagates `IsADirectoryError`; the deployment-level effect was not tested.

The session is `web-assessment-20261003-232929.040043307-000002` under `sessions/web/claims-desk-heldout-20261004/`. It completed in about 18 minutes, with 25 assessment model calls, four completed worker tasks, three accepted coordinator plans, and two rejected coordinator responses that required correction. The first rejection occurred because a registered source location was omitted from the finding's parallel evidence list; a generic normalization fix and regression test now add that already-cited artifact when it is registered and valid. The second rejection caught a model-invented evidence path and was correctly repaired by the existing correction flow. The final report's two reproduced findings cite source captures and separate verifier logs; the candidate remains clearly unconfirmed.

This is one successful source-review smoke test, not a measured detection rate or evidence that arbitrary deployed applications are covered. The fixture has only four read routes and no hosting adapter. The final coordinator input was 103.3 KiB of the configured 128 KiB application allowance, so a small two-round assessment already exercised much of the coordinator window. The correction calls and elapsed time are real efficiency costs to investigate, without cutting evidence that changes a security conclusion.

Comparative runs need a scope distinction: an unauthenticated review cannot establish the behavior of authenticated workflows or third-party integrations. Future comparisons should state which workflows and roles were actually exercised and show these coverage gaps prominently before comparing finding counts.
