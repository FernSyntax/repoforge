# Configuration

How to write `repoforge.yaml` (engine repositories) or
`.repoforge/config.yml` (generated projects — same schema).
Exhaustive field semantics: `docs/architecture/configuration-model.md`.
Schema source of truth: `schemas/repoforge.schema.yaml`
(canonical) and `.json` (derived, identical).

## Minimal policy

```yaml
standard:
  name: "repoforge"
  version: "v1"
repository:
  governance_profile: "solo"   # solo | team | strict
  visibility: "public"
  default_branch: "main"
ownership:
  managed_paths:
    - LICENSE
```

Everything else falls back to documented defaults (mostly the
secure, standard option). Unknown fields are rejected — typos fail
loudly instead of being ignored.

## Sections

- **`repository`** — `governance_profile` selects the ruleset and
  review posture (`solo`: PR flow without self-approval theater;
  `team`: one approval plus code-owner review; `strict`: two
  approvals plus signed commits). `visibility` is declared but
  never changed by automation.
- **`merge`** — squash / merge-commit / rebase toggles,
  `delete_branch`, `linear_history`, `auto_merge`. The PR rule's
  allowed methods derive from these, so repository and ruleset
  always agree.
- **`git`** — `commit_style: conventional` and
  `branch_style: type-issue-description` (the only v1 options).
- **`github`** — `issues`, `discussions`, `projects`, `wiki`
  toggles. Bootstrap converges them; doctor audits them.
- **`security`** — booleans with secure defaults (`dependabot`,
  `secret_scanning` + push protection, `dependency_review`,
  `private_vulnerability_reporting`, `scorecard` default true;
  `code_scanning` default false). `action_pinning: sha` enforces
  full-length commit SHAs (spec.md §8.6). Plan-limited features become
  explicit diagnostics, never silent skips.
- **`release`** — `strategy: github-release` (annotated tags plus
  a GitHub release per tag).
- **`project`** — `kind` classifies the repository (e.g. `cli`,
  `application`).
- **`ownership`** — `managed_paths` (tooling converges),
  `scaffolded_paths` (created once, then yours),
  `user_owned_paths` (never touched), `placeholder_paths`
  (intentional double-brace content exempt from checks).

## Checking your work

```sh
repoforge validate        # schema, files, ownership, commits, branches, YAML, secrets, links
repoforge bootstrap --dry-run   # what would change, without changing it
```
