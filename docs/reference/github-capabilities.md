# GitHub Capabilities by Plan

Reference for what the governance model can rely on, per GitHub plan
and repository visibility. Sources: GitHub's plan documentation, the
rulesets API documentation, and the Advanced Security documentation, as
of writing. Re-verify before relying on a row (spec.md §7.2); GitHub
changes plan boundaries over time.

## The four capability states

| State | Meaning | Tooling behavior |
| ----- | ------- | ---------------- |
| **supported** | Usable on this repository now. | Apply/verify normally. |
| **unavailable** | Does not exist in this context (no plan grants it). | Hard diagnostic if policy references it. |
| **unsupported-by-plan** | Exists; current plan excludes it. Upgrade path exists. | `required` → hard diagnostic naming the plan/product; `preferred` → explicit warning. |
| **intentionally-disabled** | Policy sets it `disabled`. | Reported as a recorded decision. |

"Silent skip" is not a state. Every referenced capability resolves to
exactly one of the four.

## Matrix

Legend: ✓ supported · Ⓟ unsupported-by-plan (upgrade path named) ·
✗ unavailable (no plan grants it here)

| Capability | Public repo, any plan | Private: Free (personal) | Private: Pro | Private: Team (org) | Private: Enterprise |
| ---------- | --------------------- | ------------------------ | ------------ | ------------------- | ------------------- |
| Repository settings, topics, features (issues/projects/wiki) | ✓ | ✓ (wikis Ⓟ→Pro) | ✓ | ✓ | ✓ |
| Merge method settings, delete-branch-on-merge, auto-merge | ✓ | ✓ | ✓ | ✓ | ✓ |
| Branch/tag rulesets | ✓ | Ⓟ → Pro/Team | ✓ | ✓ | ✓ |
| Ruleset enforcement `evaluate` | Ⓟ → Enterprise | Ⓟ → Enterprise | Ⓟ → Enterprise | Ⓟ → Enterprise | ✓ |
| Metadata restriction rules (commit/tag patterns) | Ⓟ → Enterprise (org) | Ⓟ → Enterprise | Ⓟ → Enterprise | Ⓟ → Enterprise | ✓ |
| Push rulesets | ✓ | Ⓟ → Team | Ⓟ → Team | ✓ | ✓ |
| Required reviewers / code owners | ✓ | Ⓟ → Pro | ✓ | ✓ | ✓ |
| Dependabot alerts | ✓ | ✓ | ✓ | ✓ | ✓ |
| Dependabot security updates | ✓ | ✓ | ✓ | ✓ | ✓ |
| Dependabot malware alerts² | UI-only | UI-only | UI-only | UI-only | UI-only |
| Private vulnerability reporting | ✓ (free) | unclassified¹ | unclassified¹ | unclassified¹ | unclassified¹ |
| Secret scanning | ✓ (free) | Ⓟ → Secret Protection | Ⓟ → Secret Protection | Ⓟ → Secret Protection add-on | ✓ |
| Secret scanning push protection | ✓ (free) | Ⓟ → Secret Protection | Ⓟ → Secret Protection | Ⓟ → Secret Protection add-on | ✓ |
| Code scanning (CodeQL default setup) | ✓ (free) | Ⓟ → Code Security | Ⓟ → Code Security | Ⓟ → Code Security add-on | ✓ |
| Dependency review enforcement | ✓ (free, via action) | Ⓟ → Code Security | Ⓟ → Code Security | Ⓟ → Code Security add-on | ✓ |
| Dependency graph | ✓ | ✓ | ✓ | ✓ | ✓ |
| `internal` visibility | ✗ (user repos) | ✗ | ✗ | ✗ | ✓ (Enterprise orgs) |

GitHub Advanced Security is sold as two products — **GitHub Code
Security** and **GitHub Secret Protection** — add-ons for Team,
included in Enterprise.

¹ Private vulnerability reporting is documented for public
repositories; elsewhere its availability is undocumented, so tooling
deliberately does not pre-classify it: bootstrap attempts the change
and surfaces API errors explicitly, doctor reports the probed state
as drift. No silent outcome in either direction.

² Dependabot malware alerts have no REST API (open upstream issue
`github/rest-api-description#6251`, confirmed 2026-10: no
`security_and_analysis` field, no dedicated endpoint). Tooling can
neither read nor change the toggle on any plan — enable it in
Settings → Advanced Security (requires Dependabot alerts). It is
deliberately absent from policy, bootstrap, and doctor so no audit
ever claims coverage it does not have.

## Detection methods

All detection is read-only (doctor-safe, spec.md §11.2):

| Capability | How tooling classifies it |
| ---------- | ------------------------- |
| Repository/merge settings | `GET /repos/{owner}/{repo}` fields; owner `type` (User vs Organization) and visibility qualify plan-dependent rows. |
| Rulesets | `GET /repos/{owner}/{repo}/rulesets`; 403/404 with plan messaging on a Free private repository classifies unsupported-by-plan. |
| Secret scanning / push protection | `GET /repos/{owner}/{repo}` `security_and_analysis` statuses (visible to admins). |
| Code scanning | `GET /repos/{owner}/{repo}/code-scanning/default-setup` (or its 404/403). |
| Dependabot | `GET .../vulnerability-alerts` (204 = enabled, 404 = disabled); same pattern for `automated-security-fixes`. |
| Private vulnerability reporting | `GET .../private-vulnerability-reporting` returns `{"enabled"}` (a 404 reads as unconfirmed-disabled, reconciled or failed explicitly on apply). |
| Plan-restricted rules (commit metadata and branch/tag name patterns, evaluate, push rulesets) | Probe create/update is **not** read-only, so classification is by documented plan boundaries + owner/plan signals, confirmed on first apply error, which must be translated into the diagnostic format below. The pattern rules are declared in the governance profiles but deliberately not realized in the canonical ruleset payloads (`configs/github/rulesets/`). |

## Diagnostic contract

When classification is unsupported-by-plan or unavailable and the
policy says `required`, tooling must emit (and exit non-zero on):

1. the setting path (e.g. `security.code_scanning`),
2. the detected state,
3. the blocker in plain language, including the required plan/product,
4. the policy source (file/profile),
5. the remedy: upgrade, or explicitly lower the setting to
   `preferred`/`disabled`.

A bare HTTP error code is never an acceptable diagnostic. Security
features never fail silently: an unverified `required` setting is a
failed run, not a warning.
