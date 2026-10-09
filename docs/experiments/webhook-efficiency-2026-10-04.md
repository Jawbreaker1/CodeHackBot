# Webhook assessment after the planning change

This is one end-to-end GUI comparison against the [blind webhook baseline](webhook-heldout-2026-10-04.md), not a platform benchmark. The same local synthetic target, user prompt, **Focused** depth, Daybreak Blue model, and per-action approval mode were used. The new session is `webhook-efficiency-20261004 / web-assessment-20261004-144702.187179717-000002`.

The coordinator assigned **one** first-round worker to trace the uncharacterized gateway and test its strongest lead. In the baseline it assigned two initial workers who duplicated the source trace and short-signature test. The first worker found the same one-character HMAC-prefix flaw with positive and negative controls. The coordinator then assigned two distinct later-round tasks: an independent challenge of that flaw and a timestamp/replay check. The challenge supported the high-severity finding. The replay worker directly observed that stale, future, missing-timestamp, and repeated correctly signed synthetic events were accepted; the final report left that finding as a **candidate** because no independent challenge of it ran. The report did not present a code-level observation as deployment-level impact.

| Measure | Baseline | New run |
| --- | ---: | ---: |
| Initial workers | 2 | 1 |
| Later-round workers | 1 | 2 |
| Elapsed time | 9m 40s | 12m 31s |
| Model calls | 18 | 18 |
| Reported tokens | 226,678 | 233,240 |
| Reproduced findings | 1 | 1 |
| Additional candidates | 0 | 1 |

The initial split improved, but the full run was **2m 51s slower** and used slightly more tokens. The extra replay investigation makes the runs different in scope; this result cannot isolate the effect of the worker-count instruction on latency. It does show that the coordinator preserved a later independent challenge and that the finding validator prevented the replay lead from being overstated. The final coordinator decision needed structured correction after it initially tried to mark both findings reproduced without the required challenge verdict. Those correction calls are a plausible source of avoidable latency, but this single run does not quantify their share.

I inspected the persisted `assessment.json`, the report, the coordinator requests and rejection records, and worker context snapshots. The first worker's step-3 context packet was about 26 KiB, and the verifier's initial packet about 33 KiB; the latter included roughly 12 KiB of retrieved task knowledge. Both are well below the configured 128 KiB app input ceiling. The session completed with 18 calls, zero failed calls, and three successful worker results. The final report and evidence remain in the session directory. Repeat this comparison on an unseen larger target before claiming a general speed or discovery improvement.
