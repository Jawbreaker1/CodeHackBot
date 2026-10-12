# Comparative assessment evaluation

Use this procedure to measure whether BirdHackBot finds real weaknesses across unlike tasks without increasing false claims. It is for controlled fixtures with independently known truth. A public site with unknown vulnerabilities cannot be given a meaningful "missed issues" count.

Before any agent sees the fixture, freeze its vulnerable and fixed states, the operator brief, permitted tools and budget, and an independent truth file. Keep that file **outside every directory readable by an assessment worker**; do not include it in the target source tree, a worker workspace, a model prompt, or a reusable helper. Give each issue a stable ID for its root cause. Include fixed controls so a false positive has a visible cost. Retain failed and incomplete runs.

After the run, a reviewer examines the actual target behavior, commands, evidence, and latest finding claims. The reviewer writes one verdict per current finding. A `verified` verdict requires an independently checked reproduction or other decisive observation; a cited path, scanner label, source hypothesis, or model explanation alone is insufficient. A `false` verdict records a contradicted claim. Use `inconclusive` when the evidence cannot decide. Map each verified claim to one **present** truth issue; repeated claims about the same root cause share that ID. The review file must match the exact assessment ID and finding titles, so scoring a later revised report with an old review fails.

Example truth file, held outside worker reach:

```json
{"fixture_id":"review-lab-v1","issues":[
  {"id":"cross-role-read","present":true},
  {"id":"fixed-export-control","present":false}
]}
```

Example review of a report containing one reproduced finding:

```json
{"fixture_id":"review-lab-v1","assessment_id":"assessment-id-from-json",
 "operator_interventions":0,"target_effects":"none",
 "claims":[{"finding_index":0,"title":"Exact current finding title",
   "verdict":"verified","issue_id":"cross-role-read",
   "reason":"Independent role-separated request and response matched the saved evidence"}]}
```

Run from the repository root after the assessment and peer review:

```sh
python3 scripts/score_assessment.py /path/to/assessment.json /private/truth.json /private/review.json
```

For BirdHackBot, the scorer reads only the latest coordinator decision. For a comparator without BirdHackBot's session format, transcribe its final claims into a small run JSON with `id`, `model`, `status`, `run_completed` (boolean), `findings` (an array of objects with exact `title` and `status`), and, when available, `started_at`, `finished_at`, and `usage`. Keep the comparator's original transcript and evidence separately for review; do not invent unavailable usage values. The same truth and review format then scores both systems.

The scorer reports unique `verified_reproduced` issues separately from `verified_candidate_only` leads. Present truth issues without a verified claim are `missed_issues`. It also reports false and inconclusive claim indexes, false reproduced claims, false claims against fixed controls, duplicates, run completion, time, model calls, and provider-reported tokens. It rejects missing reviews, changed titles, unknown issue IDs, and a claim that a fixed control was verified. **The scorer does not read target evidence or decide whether a review verdict is correct**; that remains the reviewer's responsibility.

Compare BirdHackBot, direct Codex, and any specialist baseline under matched model/tool/source access and aggregate budgets where possible. Record differences that cannot be matched. Repeat each configuration at least three times before making a narrow performance claim, and report variation, operator interventions, side effects, misses, false claims, and time alongside unique verified findings. Use varied, held-out web, source, network/service, configuration, identity, and advisory scenarios. Do not tune the harness to the truth IDs or let fixture-specific instructions enter its strategies. The broader [acceptance gate](acceptance-gates.md) remains open until those comparisons are completed.
