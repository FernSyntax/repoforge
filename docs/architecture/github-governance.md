# GitHub Governance Model

Architecture document. Explains how RepoForge models GitHub
governance declaratively. Normative behavior: spec.md §7, 8, 10,
11. Field semantics: `schemas/github-governance.schema.yaml` (canonical)
with the plan-availability matrix in
`docs/reference/github-capabilities.md`.

## Why a separate model

The repository contract (`schemas/repoforge.schema.yaml`) states
*that* a repository has a governance profile; this model states *what
the profile configures on GitHub*. Separation keeps the contract
platform-agnostic and concentrates every GitHub-specific field name,
endpoint, and plan restriction in one layer — the only layer that talks
to the GitHub API (spec.md §7).

## Artifacts

| File | Role |
| ---- | ---- |
| `configs/github/repository.yaml` | Baseline: plan-independent repository and merge settings every managed repository shares. No rulesets (unavailable on Free-plan private repos). |
| `configs/github/solo.yaml` | Profile for one maintainer: PR flow with `required_approvals: 0`, status checks, conversation resolution, linear history, ref protection, security features required where free. Deliberately below the OpenSSF Scorecard Branch-Protection bar: a single maintainer cannot supply a non-author approval, and the standard does not pretend otherwise. |
| `configs/github/team.yaml` | Profile for teams: 1 approval, stale-review dismissal, last-push approval, conversation resolution, code-owner review, all security features required. This is the scorecard-maximal branch-protection shape. |
| `configs/github/strict.yaml` | High-assurance profile: 2 approvals, last-push approval, signed commits, linear history, metadata restrictions, SemVer tag shape (the pattern rules are Enterprise-plan: declared as intent, not realized in the canonical payloads below). |
| `configs/github/rulesets/{main-solo,main-team,main-strict,tags}.json` | Canonical ruleset payloads: the exact create/update bodies bootstrap applies and doctor audits, validated against the documented ruleset request schema. The profile's `rulesets` section must agree with them; `configs/rulesets_test.go` cross-checks. |
| `schemas/github-governance.schema.yaml` / `.json` | v1 schema (canonical/derived) all four configs validate against. |

Profiles layer onto the baseline: tooling deep-merges `repository.yaml`
with the selected profile, profile values winning. The contract's
`repository.governance_profile` (`solo` | `team` | `strict`) selects
the profile.

## Model-to-API mapping

Endpoint and field names below are per the documented REST API at
writing time; implementation must re-verify (spec.md §7.2).

| Model section | GitHub API realization |
| ------------- | ---------------------- |
| `repository` (visibility, description, default_branch, features) | `PATCH /repos/{owner}/{repo}` (`has_issues`, `has_wiki`, `has_projects`, `has_discussions`, …) |

Every governance profile sets `features.issues: true` and
`features.wiki: true`, and a profile document that sets either to false
is rejected at load time. The rationale is structural: Issues are the
unit of work in the issue-driven workflow (spec.md §12), and the wiki is
the seeded documentation surface that `repoforge wiki sync` maintains
(spec.md §13.9). A repository that opts into the standard by declaring a
governance profile must therefore keep both enabled - `validate` rejects
a policy that does not, so the requirement is mechanical rather than
advisory.
| `repository.topics` | `PUT /repos/{owner}/{repo}/topics` |
| `merge.allow_*`, `delete_branch_on_merge`, `allow_auto_merge` | `PATCH /repos/{owner}/{repo}` (`allow_squash_merge`, …) |
| `merge.require_linear_history` | ruleset rule `required_linear_history` on the main ruleset |
| `merge.allow_*` (again) | ruleset `pull_request` rule `allowed_merge_methods` — derived, must agree with repo settings |
| `rulesets.main` | branch ruleset, `conditions.ref_name.include: ["~DEFAULT_BRANCH"]`, rules: `pull_request`, `required_status_checks`, `required_linear_history`, `required_signatures`, `non_fast_forward`, `deletion` (the declared `commit_message_pattern` / `commit_author_email_pattern` restrictions are an Enterprise-plan capability and are not realized in the canonical payload) |
| `rulesets.release_tags` | tag ruleset over `patterns`, rules: `deletion`, `update` (the declared `tag_name_pattern` is likewise Enterprise-plan and not realized) |
| `security.dependabot_alerts` | `PUT|DELETE /repos/{owner}/{repo}/vulnerability-alerts` |
| `security.dependabot_security_updates` | `PUT|DELETE /repos/{owner}/{repo}/automated-security-fixes` |
| `security.secret_scanning`, `secret_scanning_push_protection` | `PATCH /repos/{owner}/{repo}` `security_and_analysis` statuses |
| `security.code_scanning` | `PATCH /repos/{owner}/{repo}/code-scanning/default-setup` upsert with `{"state": "configured"}` (CodeQL default setup, languages auto-detected; there is no POST create route) |
| `security.dependency_review` | dependency graph + `dependency-review-action` in CI |

## Security tri-state: no silent failure

Every `security.*` field is `required` | `preferred` | `disabled`,
never boolean:

- **required** — the capability must be applied and verified. Any
  failure, including plan restrictions, is a hard diagnostic naming the
  setting, the blocker, and the remediation (upgrade plan, or lower the
  setting explicitly). Bootstrap exits non-zero; doctor reports drift.
- **preferred** — applied when the capability exists; when it does not,
  the outcome is an explicit warning finding, not silence.
- **disabled** — intentionally off. The intentional state is itself
  reported, so "off" is always a decision on record, never an accident.

This is the mechanism behind "security features must not silently
fail": the failure mode of a boolean (assumed-on, actually-off) is
impossible to express.

## Capability detection

Capabilities are **detected, never assumed** (spec.md §7.1: the API is the
source of truth). Before planning, tooling classifies every referenced
capability into one of four states:

- **supported** — available on this repository/plan now.
- **unavailable** — does not exist in this context at all (e.g.
  `internal` visibility outside Enterprise organizations; code owners
  without a CODEOWNERS file).
- **unsupported-by-plan** — exists, but the current plan tier excludes
  it (e.g. rulesets on a Free-plan private repository; secret scanning
  on a private repository without GitHub Secret Protection). The
  diagnostic names the required plan/product.
- **intentionally-disabled** — policy sets it `disabled` (or omits it
  under a default); reported as a decision, not an error.

Detection is read-only (GETs and documented status objects), so doctor
can perform it without mutating (spec.md §11.2). Detection results are
per-invocation; no cached plan assumptions.

## Diagnostics

A plan restriction must read like:

```
security.secret_scanning: unsupported-by-plan
  state:    disabled (cannot enable)
  blocker:  private repositories require GitHub Secret Protection
  policy:   required (configs/github/team.yaml)
  remedy:   enable the product for this repository, or set
            secret_scanning: "preferred"|"disabled" explicitly
```

Never "403 Forbidden" alone. Never a skipped check.

## Ruleset ownership convention

RepoForge owns exactly two rulesets, identified by deterministic
names: **`repoforge:main`** (target `branch`, conditions
`~DEFAULT_BRANCH`) and **`repoforge:release-tags`** (target `tag`,
conditions `refs/tags/v*`). The rulesets API exposes no metadata or
description field, so the name *is* the ownership metadata; the
`repoforge:` prefix keeps the names unambiguous beside
user-created rulesets (a legacy ruleset named `main` is unmanaged by
construction). Constants: `configs.MainRulesetName`,
`configs.ReleaseTagsRulesetName`.

Desired state lives in `configs/github/rulesets/*.json` (canonical
payloads). `configs.DesiredRulesets` layers two repository-specific
overlays on top: required status-check contexts discovered from the
job names of workflows that run on pull requests, plus — when the
contract requires code scanning — the CodeQL default-setup analysis
contexts for the languages the repository declares (`Analyze (go)`
for a Go module, `Analyze (actions)` for workflow files), and
`pull_request.allowed_merge_methods` derived from the contract merge
settings. The `required_status_checks` rule is dropped when no
contexts exist; `configs/rulesets_test.go` cross-checks that each
governance profile document agrees with its canonical payload.
Bootstrap and doctor both compose their wants through it, so they
always answer from the same desired state.

Ownership of a named ruleset is total: bootstrap's update replaces the
ruleset wholesale, so drift is any difference in `enforcement`,
`conditions.ref_name`, the rule-type set, the parameters RepoForge
sets, or bypass actors — including rules present remotely but absent
from the payload. Bypass actors are owned as an explicit empty list
(nobody bypasses, including repository administrators); they are
compared only when the API returns them, because the property is
reported exclusively to callers with write access. Documented defaults
*inside* a rule that RepoForge does not set (for example
`do_not_enforce_on_create`) are never compared, so a conforming
ruleset cannot report perpetual drift.

Bootstrap creates and updates only the two owned rulesets; any other
ruleset is unmanaged — reported as `conflict` (repository-level) or
`report` (inherited from an organization) and never modified or
deleted. Doctor audits the same two names: one PASS finding when an
owned ruleset exists and matches its payload, one FAIL per drift
reason (current value, policy value, remediation
`repoforge bootstrap`), and a WARN for unmanaged rulesets that
bootstrap will report as conflicts and leave untouched.

## Repository labels

Repositories carry the canonical label set in
`configs/github/labels.yaml` (v1): `type:*` (6), `priority:p0..p3`
(4), `area:*` (6), `status:*` (3), each with a fixed color
(hexadecimal, no leading `#`) and a short description. Ownership is
by exact name — the labels API exposes no metadata field, so a label
with a canonical name *is* RepoForge-owned.

Bootstrap creates missing canonical labels and updates color and
description on drifted ones (labels are addressed by current name and
never renamed). Any other label is user-owned: preserved, never
modified, and never deleted — the client has no label-delete path at
all. Doctor audits the canonical names: one PASS when all exist and
match, one FAIL per missing or drifted label with the
`repoforge bootstrap` remediation; unrelated labels are ignored.

## Current status

`repoforge bootstrap` (plan → confirm → apply → verify, dry-run
first, spec.md §10) and `repoforge doctor` (read-only audit, spec.md §11)
consume these documents. The configs validate against the schema in CI
(`Validation` workflow) and are validated locally on change
(`go test ./internal/config/`).
