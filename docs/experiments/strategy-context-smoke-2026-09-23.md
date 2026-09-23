# Strategy and context GUI smoke — 2026-09-23

The first run below is diagnostic, not a recovery acceptance pass. The target was Johan's
local `secret.zip` in the closed Kali lab. The unchanged user request asked the
coordinator to choose its own tools and strategy, verify access to an entry,
and omit the password from chat. No network target was in scope.

The browser run is saved in
`sessions/web-strategy-v2/strategy-v2/web-assessment-20260923-101500.289135857-000002/`.
The first worker used the structured `load_strategy` decision to retain the
credential-recovery guide with its source checksum, identified the archive,
derived a restricted PKZIP representation, and ran the complete unmodified
RockYou list. Its initial timing wrapper used an unavailable binary; it
corrected the wrapper on its next action. The full list missed, and the worker
reported only that tested candidate family as a negative result.

The coordinator then proposed two independent follow-ups: transformed
dictionary candidates and structured masks. The operator selected the mutation
worker alone to isolate this diagnostic. That worker verified its input and
previewed an installed John rule, but its fourth model decision failed while
waiting for the local subscription bridge. No mutation search was executed.
The saved terminal packet records `Client.Timeout exceeded while awaiting
headers`. The coordinator's third plan accurately called this an internal
model-service timeout, proposed a fresh mutation worker and an independent
mask worker, and reserved separate validation if a credential is found. This
demonstrates replanning from both a method-limited miss and an infrastructure
failure; it does not demonstrate successful recovery.

The web client's default HTTP timeout was 90 seconds, shorter than the
subscription bridge's three-minute model budget. Subscription calls now allow
200 seconds while ordinary local-model calls retain their existing default.
This removes one premature cancellation path. The fresh live run below confirms
the whole recovery sequence once, but does not establish repeatability.

The context debugger made a specific cost visible. The mutation worker's
fourth packet was about 41 KiB (roughly 10.3k tokens by the app's character
estimate), below the 128 KiB configured input ceiling. About 11 KiB was its
behavior frame, 8.5 KiB its dependency handoff, 4.3 KiB recent results, and
2.3 KiB strategy guidance. These are independent per-worker packets, not a
single shared coordinator/worker conversation. The dependency card and repeated
behavior frame are good candidates for later measured compression, but their
contents should be reviewed in the debugger before changing the handoff.

The debugger was exercised through the browser against a copy of this saved
session at `http://127.0.0.1:8085/context` so the live run was not modified.
It displayed coordinator turns and ordered worker packet sections, opened the
retained strategy text, and applied then reversed an optional-section omission
on the copied session. This older run predates worker exact-request capture,
so its worker turns show packet snapshots; new runs also record the ordered
messages passed to the model client. The debugger is restricted to local
loopback requests.

## Fresh end-user run on the updated build

A second browser session, saved under the local temporary session root
`/tmp/birdhackbot-strategy-v3/strategy-v3/web-assessment-20260923-104242.515909718-000002/`,
used the same unassisted request and `gpt-daybreak-blue-latest` through the
subscription bridge. The operator chose the session's dangerous-actions-only
approval mode, reviewed scoped actions in the GUI, and approved recovery and
validation steps. The coordinator produced five plan rounds and five completed
workers. It corrected an invalid initial RockYou test when the first worker
passed compressed bytes directly to John; a correctly decoded 14,344,392-entry
plain pass and an independent 3,270,667-candidate focused mutation pass then
ran in parallel and missed. The coordinator treated both as method-limited
results and proposed a broader rule pass plus a bounded mask search. The
operator selected the rule pass; the mask task was skipped because it was no
longer needed.

The rule worker used the installed Best64 rules against the decoded RockYou
source. Its recorded campaign reports a restricted recovered candidate after
14.479337 seconds; the campaign stopped on success, so this is not evidence
that the full rule space was exhausted. A separate dependent worker used
Python's independent `zipfile` reader to read the single encrypted entry fully
in memory, verify the ZIP CRC, and confirm the archive SHA-256 remained
`af82d38ac307097ed052739d20487fbbe9231f725993983f62a866ddb49225dd`.
No password, entry name, or entry contents were displayed or extracted. The
session reached `completed` with 47 recorded model calls and zero failed calls;
the final coordinator conclusion and five-round plan history were visible in
the GUI. The full run took about 36 minutes, much longer than the 14.5-second
successful tool call. Worker planning, repeated evidence review, model latency,
and approval pauses therefore remain important efficiency targets. This is a
single successful application path, not a general effectiveness benchmark.

The new live context debugger at `http://127.0.0.1:8086/context` showed the
coordinator and individual worker turns with ordered packet sections and the
exact worker messages passed to the model client. The final worker input was
49.3 KiB against the application's 128 KiB byte ceiling. That byte figure is
not a token-context measurement; provider usage is separately recorded in the
assessment. Optional omissions affect future worker model projections only;
the full saved context and evidence remain intact.
