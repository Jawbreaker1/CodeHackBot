---
name: configuration-exposure
description: Use when deployment settings, exposed files, storage, permissions, or secret handling may create unintended access or data exposure.
---

# Configuration and exposure review

1. Identify the intended boundary for the host, service, repository, or storage location. Record the configuration source and deployed version; sample files or defaults may not reflect the live target.
2. Look for exposure paths such as unintended listeners, permissive access controls, publicly reachable administrative surfaces, debug modes, backup artifacts, and credentials in accessible material. Use minimal non-sensitive proof instead of copying whole datasets.
3. Separate a risky setting from demonstrated reachability and impact. Validate from the relevant role or network position with the least intrusive check, and capture the exact configuration and observation.
4. Treat found credentials as sensitive evidence. Do not use them to cross into a new target or role without scope and approval.
5. Prioritize fixes by reachable impact and operational context; note compensating controls and uncertainty.
