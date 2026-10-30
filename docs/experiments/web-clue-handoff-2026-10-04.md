# Web clue handoff review

A held-out public web assessment exposed a general weakness in evidence handoff. A worker had saved a relevant source observation, but the model saw only the first 8 KiB of a 46 KiB command result. The decisive excerpt sat later in the output. Other observations reached the coordinator separately, yet the run did not connect them into a validated finding.

The executor preview now retains up to 64 KiB of medium output, while the model-view budget can shorten older observations. Full logs remain available for later recall. A regression test checks that an observation beyond the former 8 KiB boundary reaches the worker packet.

This is a handoff improvement, not proof that the harness will recognize every security path. Follow-up validation should use unrelated, unseen tasks and check whether workers link evidence across components, pursue bounded tests, and avoid unsupported claims. The detailed target notes remain in local, ignored session evidence rather than published project documentation.
