# Configs

Declarative configuration documents shipped with the standard. All files
here are data, validated against versioned schemas in `schemas/`; they
contain no credentials and no per-owner identity values (spec.md §8.4).

## github/

The GitHub governance model (docs/architecture/github-governance.md):

- `repository.yaml` — plan-independent baseline.
- `solo.yaml`, `team.yaml`, `strict.yaml` — governance profiles layered
  onto the baseline, selected by the contract's
  `repository.governance_profile`.

Every file validates against `schemas/github-governance.schema.yaml`
(v1). The profiles are consumed today: `bootstrap` (dry-run first)
applies them to GitHub and `doctor` (read-only) audits against them.

## github/labels.yaml

The canonical repository label set (v1): 19 deterministic labels
(`type:*`, `priority:p0..p3`, `area:*`, `status:*`) with fixed colors
and descriptions. Loaded strictly by `configs.CanonicalLabels`
(unknown fields rejected, duplicate names rejected); invariants pinned
in `configs/labels_test.go`. Bootstrap creates missing labels and
updates drifted color/description; doctor audits every canonical
name. Non-canonical labels are user-owned and ignored.

## github/rulesets/

Canonical ruleset payloads — the exact request bodies bootstrap
creates and updates (`repoforge:main`, `repoforge:release-tags`)
and doctor compares against. One `main-<profile>.json` per governance
profile plus the shared `tags.json`. Two repository-specific overlays
are applied at runtime by `configs.DesiredRulesets`: status-check
contexts discovered from the job names of workflows that run on
pull requests (dropped when none exist; jobs in scheduled, tag-only,
or manual workflows can never report a PR check, so requiring them
would block every merge) and `pull_request.allowed_merge_methods` derived from
the contract merge settings. Each profile's `rulesets` section is
cross-checked against its payload in `configs/rulesets_test.go`.

Enterprise-only pattern rules (`commit_message_pattern`,
`commit_author_email_pattern`, `tag_name_pattern`) are declared in the
governance profiles as intent but deliberately not realized in these
payloads — applying them would fail on non-Enterprise repositories.
