# Playbook: Encrypted ZIP Access (Lab)

Goal
- Validate whether an encrypted archive is resilient to authorized cracking attempts.

Pre-flight
- Confirm explicit permission to test the archive.
- Ensure the archive contains only lab/synthetic data.

Safe Defaults
- Use wordlists and rate limits appropriate for the lab.
- Do not attempt on third-party data.

Steps (Example)
1) Identify the archive type and encryption.
2) Use a limited wordlist to test basic resilience.
3) Stop if cracking exceeds agreed time bounds.

Evidence
- Log the method, wordlist used, and outcome.
- Do not expose recovered secrets; record a proof-of-access token instead.
