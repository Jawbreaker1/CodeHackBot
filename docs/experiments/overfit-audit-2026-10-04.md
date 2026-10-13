# Audit of scenario-specific logic

The SecurityGuru miss prompted a focused audit of the changes made to preserve and investigate security clues. The question is whether the runtime now follows general evidence, or whether it has been taught one URL-token/analytics scenario.

## Code and prompt review

- The production Go runtime has no SecurityGuru, Matomo, magic-link, invitation, or `secret.zip` branch, target matcher, or command parser. The 64 KiB tool-output preview applies to all commands and file types. Full logs remain available for recall; the model view can shorten older observations when the selected input allowance is reached.
- `Lead` is a model-authored, evidence-linked hypothesis with a proposed next check. Its validator checks references and continuity, not vulnerability keywords. Worker `lead_hints` and coordinator `open_leads` use the same generic path for all assessment domains.
- The source-review and web-application guides briefly became too specific about URL parameters, browser storage, telemetry, and third-party scripts. They now direct workers to trace material data and control flows across components and decisions, with browser startup order considered only when relevant. The coordinator asks for one consequential input-to-effect path, not a fixed scan sequence or a named sink.
- The lead-continuity unit fixture formerly used a callback URL and third-party request. It now uses an unrelated cross-tenant record-filter hypothesis. This test checks evidence continuity, not detection quality.

## Behavioral checks

- A prior blind GUI source review of two fictional frontend builds distinguished the source path that would include a synthetic invitation in telemetry from a control build that removes it first. It correctly kept both as runtime-unverified candidates. This is a useful regression case but close in kind to the motivating miss.
- After the generic prompt change, an opt-in Daybreak live coordinator test combined two independent observations in a different domain: tenant-tagged diagnostic traces and an anonymous support-portal listing. It proposed a bounded synthetic-marker comparison, and cited both workers' evidence. This checks cross-worker synthesis; it does not exercise a full browser assessment.
- The tool-output regression now places a decisive marker beyond the former 8 KiB head preview and verifies that it reaches a worker's 96 KiB model packet even with older observations under pressure. It also checks that durable older evidence is not altered. Full repository CI passes.
- An earlier held-out GUI review found and independently verified a cross-tenant disclosure and a path-containment defect in a Python service. That run predates the latest wording changes and is supporting evidence of broader capability, not validation of the new prompt.

No target-specific rule was found or retained. These checks do **not** establish a reliable vulnerability-detection rate. Larger unseen tasks with different trust boundaries, negative controls, realistic source size, and observed browser/server behavior are still needed. We should compare missed leads, false candidates, evidence quality, elapsed time, and model calls before concluding that the harness matches direct Codex work.

The three-hint worker limit and eight-open-lead coordinator limit are general context/budget controls, not scenario rules. They could still discard useful competing hypotheses in a large assessment; the current checks do not establish the right limits. The 64 KiB preview similarly improves medium outputs without guaranteeing that a clue deep in a much larger log reaches the model. Both need scale tests rather than another target-specific exception.
