# Playbook: Encrypted ZIP Access (Lab)

## Goal
Assess archive password strength and recovery resistance for authorized lab ZIP files.

## Required Input
- Explicit authorization for this specific archive.
- Archive path and expected output requirements.
- Time budget and acceptable cracking intensity.

## Guardrails
- Lab-only/synthetic data.
- Bounded runtime per strategy (avoid endless loops).
- Capture proof-of-access, not sensitive content.

## Planning Checklist
1. Confirm archive path and scope.
2. Determine encryption/hash type before selecting cracking method.
3. Define strategy ladder and stop criteria.

## Procedure
1. Verify archive metadata and encryption hints.
   - Examples: `zipinfo -v <file.zip>`, `7z l -slt <file.zip>`
2. Extract crackable hash material.
   - Example: `zip2john <file.zip> > zip.hash`
3. Run strategy ladder (bounded):
   - Wordlist baseline (`john --wordlist=<list> --format=zip zip.hash`)
   - Wordlist + rules (`john --rules --wordlist=<list> --format=zip zip.hash`)
   - Targeted mask/hybrid (pattern-driven, time-boxed).
4. Optional fallback tooling if available.
   - `fcrackzip`/`hashcat` with explicit limits and progress output.
5. Validate minimal proof-of-access.
   - List/extract one known harmless file or token.

## Decision Gates
- If password recovered: stop cracking, validate minimal access, move to report.
- If time budget exceeded: stop and report resilience status with tested strategies.
- If archive type unsupported/corrupt: report limitation clearly.

## Evidence & Reporting
- Record hash type, tools, command parameters, wordlists/masks, runtime, and outcome.
- Include what was tested vs not tested.
- Store only proof token (for example, checksum of extracted marker text), not sensitive payloads.
