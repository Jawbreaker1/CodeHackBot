# Playbook: Web Enumeration (Lab)

## Goal
Build a reliable map of the target web surface: ownership, stack, routes, and likely weak points.

## Required Input
- Base URL and domain in scope.
- Allowed network mode for this session.
- Rate-limit constraints (requests/sec, max crawl pages, max depth).

## Guardrails
- Read-only enumeration by default.
- Respect robots/rate limits unless explicit approval says otherwise.
- No payload fuzzing or exploit attempts in this playbook phase.

## Planning Checklist
1. Confirm canonical target (`https://...`) and subdomain scope.
2. Define crawl bounds and stop criteria.
3. Choose passive-first recon tools before active probing.

## Procedure
1. Domain and DNS recon.
   - Examples: `whois <domain>`, `dig <domain> ANY +multiline +noall +answer`
2. TLS and header fingerprinting.
   - Example: `curl -I -v https://<domain>`
3. Structured crawl and link extraction.
   - Prefer internal tooling: `/crawl <url> max_pages=20 max_depth=2 same_host=true`
   - Then: `/parse_links` on retrieved artifacts.
4. Enumerate common safe paths with rate limiting.
   - Example categories: `/admin`, `/login`, `/api`, `/robots.txt`, `/sitemap.xml`
5. Identify likely misconfigurations.
   - Missing security headers, exposed debug endpoints, directory listing, public backup files.

## Decision Gates
- If Cloudflare/WAF present: switch to low-noise passive recon and artifact analysis.
- If new high-risk endpoint appears: create focused follow-up task (auth flow, input validation, session handling).

## Evidence & Reporting
- Log all requests and discovered routes/artifacts.
- Report findings with URL, response evidence, risk, and confidence.
- Explicitly list unknowns (blocked paths, JS-rendered routes not reached, auth-only areas).
