# Playbook: Web Enumeration (Lab)

Goal
- Discover domain ownership, tech stack, endpoints, and common misconfigurations.

Pre-flight
- Confirm target base URL/domain and scope.
- Ensure rate limits are respected.

Safe Defaults
- Avoid destructive actions; prefer read-only enumeration.

Steps (Example)
1) Domain recon (WHOIS, DNS, certificate info).
2) Identify server headers and framework hints.
3) Crawl for routes and static assets.
4) Enumerate common paths with safe wordlists (rate-limited).
5) Check for common misconfigurations (exposed admin paths, directory listing).

Evidence
- Capture WHOIS/DNS notes, headers, routes, and misconfigs in logs.
- Summarize findings in the session summary and report.
