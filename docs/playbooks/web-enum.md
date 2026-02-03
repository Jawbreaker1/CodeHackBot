# Playbook: Web Enumeration (Lab)

Goal
- Discover web endpoints, tech stack, and common misconfigurations.

Pre-flight
- Confirm target base URL and scope.
- Ensure rate limits are respected.

Safe Defaults
- Avoid destructive actions; prefer read-only enumeration.

Steps (Example)
1) Identify server header and framework hints.
2) Crawl for routes and static assets.
3) Check for common misconfigurations (exposed admin paths, directory listing).

Evidence
- Capture key headers, routes, and misconfigs in logs.
- Summarize findings in the session summary and report.
