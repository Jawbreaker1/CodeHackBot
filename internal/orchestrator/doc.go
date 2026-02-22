// Package orchestrator coordinates multi-worker task execution through an
// append-only event pipeline and materialized run state.
//
// Architecture notes:
//   - Events are written to JSONL and replayed to build deterministic state.
//   - Event cache and sequence state reduce repeated full-log scans on status paths.
//   - Evidence ingestion materializes artifacts/findings from task events.
//   - Coordinator and worker-runtime components emit structured lifecycle events
//     that drive approvals, retries, replans, and reporting.
//   - Reporting is evidence-first: findings are rendered with explicit
//     verification state based on linked artifact/log evidence.
package orchestrator
