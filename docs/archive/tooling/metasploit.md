# Metasploit (Guidance)

Purpose
- Use Metasploit for discovery and controlled validation in the lab.
- Prefer check-only or non-destructive modules before any exploit attempt.

Safe Usage Patterns
- Search first: identify likely modules based on service, platform, or keyword.
- Review module info/options before execution.
- Log all outputs and link to session evidence.

Discovery Examples (Non-interactive)
- Search by service: `service=http`
- Search by platform: `platform=linux`
- Search by keyword: `keyword=apache`

Notes
- This is guidance, not an exhaustive module list.
- Other tooling is allowed as long as it stays within scope, permissions, and safety rules.
