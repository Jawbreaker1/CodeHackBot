# Playbook: Network Scan (Lab)

## Goal
Identify live hosts, open ports, and exposed services inside approved lab scope.

## Required Input
- In-scope targets (CIDR, host list, or explicit IP range).
- Permission mode (`readonly`, `default`, `all`) and approval expectations.
- Time window and allowed scan intensity.

## Guardrails
- Stay inside configured scope and deny-list boundaries.
- Start passive/light first; only increase intensity with explicit approval.
- No DoS techniques or long-running brute-force in this phase.

## Planning Checklist
1. Confirm scope and expected output format.
2. Select safe first-pass scan profile.
3. Define escalation path if high-value targets are found.

## Procedure
1. Baseline host discovery.
   - Example: `nmap -sn <cidr>`
2. Targeted port/service scan on live hosts.
   - Example: `nmap -sV -Pn --top-ports 100 <host>`
3. Expand only where needed (focused full-port scan).
   - Example: `nmap -sV -Pn -p- <host>`
4. Capture service metadata for follow-up testing.
   - Version, banner, TLS hints, unusual management ports.

## Decision Gates
- If scan returns no hosts: verify network route/interface and scope input.
- If critical service appears (SSH/RDP/DB/admin UI): create a focused follow-up task, do not blindly continue broad scans.

## Evidence & Reporting
- Save raw logs under `sessions/<id>/logs/`.
- Record host -> open ports -> service/version mapping in summary.
- Note confidence and gaps (filtered ports, unreachable subnets, blocked probes).
