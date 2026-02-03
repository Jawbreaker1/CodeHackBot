# Playbook: Network Scan (Lab)

Goal
- Identify live hosts and exposed services within the approved scope.

Pre-flight
- Confirm scope (CIDR/targets) and permissions.
- Record plan in the session `plan.md`.

Safe Defaults
- Start with light probes; avoid aggressive timing unless approved.

Steps (Example)
1) Host discovery (ICMP/ARP where permitted).
2) Service scan on discovered hosts.
3) Version detection for confirmed open ports.

Evidence
- Log scan outputs to `sessions/<id>/logs/`.
- Summarize discovered hosts/services in `sessions/<id>/summary.md`.
