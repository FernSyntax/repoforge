# GitHub Support Matrix

What works where, for adopters. Maintainer reference with
detection methods and the diagnostic contract:
`github-capabilities.md`. Rule of the house: plan-dependent
features are detected, never assumed; anything unavailable is
explicit, never silent.

## Capability by plan

| Capability | Public, any plan | Private without products | Private with Code Security / Secret Protection |
| ---------- | ---------------- | ------------------------ | ---------------------------------------------- |
| Repository/merge/feature settings | ✓ | ✓ | ✓ |
| Branch/tag rulesets | ✓ | Needs Pro/Team | ✓ |
| Required reviews, code owners | ✓ | Needs Pro | ✓ |
| Commit/tag pattern rules, `evaluate` | Enterprise only | Enterprise only | ✓ on Enterprise |
| Dependabot alerts + updates | ✓ | ✓ | ✓ |
| Secret scanning + push protection | ✓ free | Needs Secret Protection | ✓ |
| Code scanning (default setup) | ✓ free | Needs Code Security | ✓ |
| Dependency review (workflow) | ✓ | Follows Code Security | ✓ |
| Scorecard (workflow) | ✓ | Runs; visibility follows code scanning | ✓ |
| Secret detection (workflow) | ✓ | ✓ (plan-independent) | ✓ |
| Private vulnerability reporting | ✓ | Attempted live; explicit errors | ✓ |
| Artifact attestations | ✓ | Enterprise Cloud only | ✓ / Enterprise Cloud |

`✓` = usable now. Product names = explicit `unsupported`
diagnostics when policy requires them. When the matrix cannot
answer (undocumented boundaries), tooling attempts the change and
surfaces API errors instead of pre-classifying.

## What this means for your policy

- Public repositories: everything above works; `code_scanning:
  true` and strict profiles are free.
- Private repositories without products: set plan-gated features
  to `preferred` or `disabled` explicitly — `required` fails the
  run with the upgrade path named.
- `UNSUPPORTED` in doctor output is a capability note, never a
  failure; `FAIL` is drift or an explicit blocker.
