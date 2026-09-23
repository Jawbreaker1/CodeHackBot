---
name: network-discovery
description: Use when scoped devices, routes, or host exposure must be identified before service testing, with uncertainty and scan impact made explicit.
---

# Scoped network discovery

1. Translate the approved target boundary into exact networks, addresses, and exclusions. Local routes and neighbors can suggest candidates but cannot expand scope. Identify the smallest observation that would answer the operator's question.
2. Prefer low-impact host discovery and existing interface, route, neighbor, and DNS evidence before broader probes. A missing reply is not proof a host is absent; name the protocol and coverage of each method.
3. Keep discovery, service enumeration, and vulnerability validation distinct. Move to deeper checks only for relevant observed hosts and within the agreed traffic and time limits.
4. Record the target set, source interface, method, timing, response evidence, and uncertainty. Reconcile conflicting host identities before attributing software or findings.

Use verified Kali tooling suited to the actual network. An address discovered outside scope remains out of scope.
