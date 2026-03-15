# Failure Taxonomy

Date: 2026-03-06  
Status: Active (Sprint 37 control doc)

## Purpose

Standardize how failures are classified, recovered, and terminalized to avoid ad hoc fixes.

## Classes

| Class | Typical signals | Recovery policy | Terminal mapping |
|---|---|---|---|
| `contract_failure` | malformed action, non-executable prose, missing completion fields | one bounded repair attempt, then pivot | `failed` if unrecoverable |
| `strategy_failure` | repeated no-progress steps, wrong tool path, dead-end branch | require strategy change (`pivot_strategy`) | `failed` or `not_met` with reason |
| `context_loss` | goal drift, wrong target, stale summary dominates current facts | refresh context anchors + evidence delta, then retry modified | `failed` if drift persists |
| `scope_denied` | out-of-scope host/target/path | no retry unless scope changes | `blocked` |
| `approval_blocked` | pending/denied/expired risky action approval | wait/retry only on grant; otherwise stop path | `blocked` |
| `infra_failure` | tool missing, env issue, I/O error, process interruption | bounded env/tool repair; ask user when needed | `failed` or `aborted` |

## Invariants

- Never map scope/approval failures to false “success”.
- Recovery must be explicit and bounded.
- Terminal output must include failure class + concrete next step.

## Change Control

Any new failure reason/state must update:
- `docs/runbooks/state-inventory.md`
- this file
- tests covering class -> recovery -> terminal mapping
