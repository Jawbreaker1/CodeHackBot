# Archive rerun timing audit — 2026-09-27

The local Daybreak GUI assessment `web-assessment-20260927-175119.910031704-000002` was stopped after 57.6 minutes because it had not recovered `secret.zip`. This is a performance and workflow failure, not evidence that the archive resisted all relevant recovery methods. Its source SHA-256 remained unchanged. The assessment is saved under `/tmp/bhb-progress-smoke-20260927/sessions/archive-knowledge-eval-20260927/`; restricted pots, candidate data, and archive contents are not reproduced here.

| Measure | Earlier completed rerun | This stopped rerun |
| --- | ---: | ---: |
| Wall time | 19.0 min | 57.6 min |
| Model calls | 37 | 65 |
| Reported tokens | 439,125 | 908,137 |
| Coordinator plans | 3 | 5 |
| Worker tasks | 3 | 6 |
| Reasoning request | Provider default | Explicit `high` |

The earlier run is `web-assessment-20260927-150154.410597718-000002`, saved under `/tmp/bhb-zip-retest-clean-20260927/sessions/archive-retest-20260927/`. The different reasoning request and approval interactions mean this is an observed regression, not a controlled model-speed comparison.

The first completed candidate test in the stopped run finished about 22 minutes 46 seconds after assessment start. Later, a fresh-pot campaign tested one million packaged public candidates and 3,787,587 generated variants without recovery; the command itself took about half a second. Across all six workers, recorded command execution totaled about 190 seconds, including roughly 187 seconds spent in a broad resource-inventory worker. All six workers loaded the credential-recovery guide. The missing ingredient was therefore not guide delivery or CPU speed.

The saved GUI event stream retains only the final 200 events (about 45 minutes). Within that window, 34 worker decision waits totaled about 24.9 minutes, 27 waits for per-action approval totaled about 14.3 minutes, and 25 command executions totaled about four seconds. These categories can overlap between workers and are not an additive wall-time decomposition. Approval waits include the human review of commands, particularly long generated shell scripts. The earlier 19-minute run had 29 recorded worker decisions with a 17.6-second median wait; the stopped run's retained decisions had a 26.6-second median. Explicit `high` reasoning may contribute to that difference, but the trace does not isolate its effect.

Several avoidable workflow costs are directly visible:

- A resource worker inventoried hundreds of installed files before the first recovery test. The next worker paged through its long metadata output in several model turns.
- The first expanded recovery command depended on a probe file that the worker had already removed. Its revised command was denied because the success path would put a recovered password in a `7z` process argument. Neither action tested the larger candidate set.
- A later worker created and fixture-tested a safe Python validator. The next worker started creating another validator because its handoff did not include the first helper's recorded source path.
- The worker prompts reached roughly 40–67 KiB in later turns. The model repeatedly received plan history, recent evidence, and prior-worker context while still making one decision per invocation.

The general correction is to start a bounded informative test after the minimum necessary capability check, pass reusable helper paths through worker handoffs, keep secrets out of process arguments, and separate preparation, execution, and validation when a long conditional command obscures failures. The next acceptance run should record time to first target test, model calls before that test, approval waits, worker-to-worker reuse, total wall time, and independently validated outcome. A faster negative run is not a successful recovery result.

## Focused GUI check after the first corrections

A new Daybreak Blue `high` GUI session, `web-assessment-20260927-190401.638736407-000002`, ran from 19:04:01 to 19:19:14 UTC (15 minutes 13 seconds) with approve-every-execution. It used 16 model calls and 200,693 reported tokens. Its worker loaded the credential-recovery guide, then the coordinator assigned one worker to inspect and test rather than opening with a separate broad resource-inventory worker. The first completed target candidate test occurred about 5 minutes 50 seconds after start, versus 22 minutes 46 seconds in the stopped rerun. This was a focused, operator-ended check, not a successful extraction or a controlled head-to-head comparison.

The completed candidate commands were much shorter than the session: 14,344,392 direct packaged candidates took two seconds; an installed John `Wordlist` rule pass took 28 seconds; numeric candidates of lengths 1–8 took four seconds; 21,794 archive-context-derived candidates took one second. None recovered a password. A separate earlier run against the same archive SHA-256 did recover and independently validate a candidate with the complete RockYou corpus plus John's `Best64` rules. The focused check used `Wordlist`, not `Best64`, so its negative result never exercised that winning transformation family. This is a strategy-selection gap, not evidence that Daybreak or Kali cannot perform the operation.

One proposed converter command failed because it assumed `/usr/bin/time` existed; the worker corrected it after another model turn. Plan-only model calls and command preparation still consumed minutes. The worker later proposed a 15-minute lowercase sweep despite the selected focused depth and no observed password-shape clue. The operator denied it. In the version exercised here, denial immediately marked the worker failed and discarded its narrative summary, so the final report omitted precise coverage for several completed campaigns even though their restricted logs remained. The runtime has since been changed to record denial as a non-executed action and stop that worker with a blocked, partial result that points the coordinator to its completed evidence; that change is covered by a worker-loop test but was not active in this GUI run. The guide now distinguishes named rule families, and the prompt discourages standalone plan-only turns and optional telemetry dependencies that block the primary test.
