// Package assist contains the assistant suggestion contract and runtime-facing
// parsing/normalization logic used by both CLI and orchestrator worker loops.
//
// Architecture notes:
//   - `Assistant` defines a single-turn decision interface over `Input` -> `Suggestion`.
//   - `LLMAssistant` handles model calls plus one strict JSON repair pass.
//   - `FallbackAssistant` provides deterministic, safety-biased defaults when LLM
//     output is unavailable or unusable.
//   - Suggestion parsing is layered: strict JSON -> loose coercion -> guarded
//     plain-text command recovery.
//   - Suggestion normalization enforces schema compatibility and command shaping
//     (including shell-wrapper split handling) before execution paths consume it.
package assist
