// Package scopeguard provides shared target and command scope enforcement
// primitives used by both single-agent and orchestrator execution paths.
//
// Architecture notes:
//   - Policies are built from allow/deny entries with alias expansion.
//   - Validation covers explicit task targets and command-inferred targets.
//   - Command validation supports fail-closed behavior for network-sensitive
//     commands and shell-wrapper forms (`bash -c`, `sh -c`, etc.).
//   - The package is intentionally dependency-light so it can be reused across
//     executor and orchestrator boundaries without introducing cycles.
package scopeguard
