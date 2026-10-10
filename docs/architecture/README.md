# Architecture

Design documentation for RepoForge. Documents here describe how the
system is structured and why. The normative behavior contract is
[spec.md](../../spec.md); architecture docs explain, they do not define.

## Contents

- [repository-contract.md](repository-contract.md) — the repository
  contract: schema artifacts, versioning, unknown-field detection.
- [configuration-model.md](configuration-model.md) — field-by-field
  reference for the policy schema (types, defaults, compatibility).
- [github-governance.md](github-governance.md) — the declarative GitHub
  governance model: baseline + profiles, API mapping, security
  tri-state, capability detection.
- [security.md](security.md) — the security baseline: threat model, CI
  trust boundaries, token permissions, third-party Actions, supply
  chain, and release artifact integrity.
- [overview.md](overview.md) — system map: control loop, components,
  invariants, and trust summary.
- [why-two-repositories.md](why-two-repositories.md) — why the engine
  and the template are separate repositories with a one-way
  relationship.

Further documents will be added as components are implemented.
