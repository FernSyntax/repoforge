# Limitations

What RepoForge deliberately does not do. Each entry states the reason,
so a limitation is a decision rather than an omission. No feature is
described as mandatory when it is only a recommendation.

## Template repository protection

| Limitation | Reason |
| --- | --- |
| The generated template's `main` and tags cannot be converged by `repoforge bootstrap` | The generated `.repoforge/config.yml` carries unresolved placeholders until adoption, so there is no policy to converge to. Protection is owner-configured; the exact ruleset payloads are in `docs/operations/template-publication.md`. Until they are applied the repository is **not** protected, and this is stated rather than claimed. |
| Dependency graph cannot be enabled through the API | `dependency-review-action` fails until it is enabled in Settings → Security on each repository. The generated template therefore ships no dependency-review workflow. |

## Product scope

| Limitation | Reason |
| --- | --- |
| No hosting, no service | RepoForge is a CLI (spec.md §4). |
| No language-specific tooling | The standard is language-agnostic; it wires the checks, it does not implement them. |
| No repository *content* management beyond the owned baseline | Content is the user's (spec.md §5.3). |
| One repository at a time | v0.1 operates on a single repository; there is no fleet mode (spec.md §4). |
| No backward compatibility with other repository-management tools | Out of scope (spec.md §4). |

## GitHub capabilities that require what the tool does not request

RepoForge never requests repository administration, so anything that
needs it is either performed once by the owner through GitHub settings
or reported as an explicit `unsupported` finding. It is never silently
skipped and never described as enforced.

| Capability | State | Why |
| --- | --- | --- |
| Organization-level settings | Not modelled | v1 is repository-scoped (spec.md §4). |
| Org projects, org rulesets | Not modelled | Same reason. |
| Collaborator administration | Not implemented | Requires administration; out of scope for a converger that must stay least-privilege. |
| Repository visibility changes | Never automated | Security-sensitive; doctor reports drift, bootstrap never applies it (spec.md §8.5). `github.visibility` is advisory policy, not a mutation target. |
| Enterprise ruleset rules — commit-metadata patterns, tag-name patterns | Parsed, not realised | Enterprise-plan only. The governance profiles declare the intent; the canonical payloads under `configs/github/rulesets/` do not carry it, and `configs/README.md` says so. |
| Dependency review | CI-enforced, not API-verifiable | There is no repository API to read it; `doctor` reports it as `unsupported` rather than claiming a pass. |
| Private-repo secret scanning, push protection, code scanning | Requires a product | Reported as `unsupported` on a private repository without GitHub Secret Protection / Code Security. |

## Platform behaviour RepoForge relies on

| Behaviour | Note |
| --- | --- |
| The wiki is a git repository at `<owner>/<repo>.wiki.git` | Verified against GitHub's own behaviour. A repository with its wiki disabled has no such endpoint, which is reported as a configuration problem, not an authentication failure. |
| `Contents: write` is not sufficient to commit workflow files | Workflow-file commits require `Workflows: write`. The publishing App requests both, and the runbook explains why. |
| Listing secret names does not validate a private key | Only minting an installation token proves the key works, which is why the preflight mints one. |
| Template repositories start with a fresh, unrelated history | A repository created from a GitHub template has no shared history with the template. This is documented rather than worked around. |
| `act` is local emulation only | It does not prove hosted secrets, permissions, runners, attestations, or remote API behaviour. |

## Verification limits

| Limit | Note |
| --- | --- |
| Integration and end-to-end suites are opt-in | They touch a real GitHub repository, so they require an explicit, disposable target and never run in ordinary CI. |
| Remote link checking is not part of local verification | It depends on an external network, so `scripts/verify` stays offline. |
| A missing lint tool fails rather than passes | `actionlint`, `shellcheck`, and friends are required in CI. Locally, a missing tool is reported with installation instructions and the run fails — it is never skipped silently. |
