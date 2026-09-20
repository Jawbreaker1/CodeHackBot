# Roadmap

Updated 2026-09-19. `TASKS.md` owns current work and status; this file describes future direction.

Immediate order: validate the shared worker and generic orchestrator with meaningful assessment fixtures, then expand knowledge and source capabilities. The end-to-end orchestrator structure is implemented and exercised by the built application; repeated real-model capability proof and broader product gates remain open.

## 1. Initial worker core cleanup — completed

Correct execution, cancellation, evidence, completion, and persistence defects. Keep the existing code useful while simplifying incorrect heuristics.

Cleanup is complete as a bounded slice. The 2026-09-20 worker rebuild removes competing planners/evaluators and heuristic recovery, adds model-authored plan revisions and bounded context views, and preserves budgets on resume. Full worker acceptance remains current work: meaningful multi-step validation under the confirmed provider settings, beyond the controlled recovery fixture.

## 2. Subscription-backed inference — first slice completed

A minimal authenticated local API wrapper now uses the ChatGPT subscription backend while preserving BirdHackBot's tool execution ownership. Three live worker checks passed through the requested Daybreak alias. Local-model support remains available. Broader provider features stay deferred until needed; setup and compatibility limits are in `docs/runbooks/subscription-bridge.md`.

## 3. Orchestrator as the primary product

A first guided lab flow now has run/task identities, a coordinator with up to two workers, separate workspaces, shared call budgets/evidence, dependencies, broadcast stop, declared scope, and one draft assessment report. It reuses the shared adaptive worker engine. The built application also passes a generic orchestration path with two independent tasks followed by dependent validation. Three guided synthetic-fixture runs passed with Daybreak; Qwen contract reliability remains under validation. Address assessment resume, enforced scope isolation, and independently checked findings in bounded increments.

Make this a guided application from startup. Build and revise plans when needed, research current vulnerabilities as software is discovered, and delegate useful leads for validation. The first complete flow joins discovery, advisory lookup, bounded worker tasks, and evidence-backed reporting.

Use Kali's actual installed capabilities, relevant playbooks, and existing local vulnerability resources. Add custom-tool creation and reuse through small validated increments. Design the workflow to run entirely locally from the outset; fully air-gapped operation is a required deployment milestone, including local research/dependencies and enforced external network isolation, not merely a local model setting.

The [competitive assessment](docs/competitive-assessment-2026-09-19.md) recommends visible coverage gaps in that first result, followed by small increments for targeted retesting and authenticated multi-role application testing. Demonstrate these on controlled fixtures before expanding into broad enterprise integrations.

## 4. Source-assisted assessment

Identify deployed software, acquire attributable matching source, investigate bounded questions, and validate candidate weaknesses. Distinguish local reproduction from proof that the deployed target is affected. Handle unavailable or mismatched source honestly.

## 5. Measured capability expansion

Begin baseline measurement with the first orchestrated fixture, then expand comparisons using independent fixture truth and held-out cases. The primary outcome is better discovery of verified unique vulnerabilities, measured alongside missed defects and false claims. Compare connected and air-gapped configurations separately and test which capabilities contribute. Expand browser/API tooling and research coverage where measured gaps justify them; keep operator effort, target effects, time, and aggregate usage visible.

Historical phase numbering is archived under `docs/archive/pre-core-cleanup-2026-09-19/` and is no longer authoritative.
