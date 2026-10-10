# ADR-0010: Documentation and wiki strategy

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §13.9, §20.6
- Implementation: `docs/`, `internal/wiki`, `template/docs/wiki/`

## Context

Documentation has to live somewhere a diff can reach, and some content is
more useful browsable than readable in a file tree. GitHub Wiki offers
the second but has no review, no branches, and no CI, which makes it a
poor source of truth and an excellent derived surface.

The failure mode to avoid is a wiki that quietly becomes a second,
untracked version of content RepoForge claims to generate.

## Decision

The repository is the source of truth. Normative requirements live in
`spec.md`; design lives in `docs/architecture/`; decisions in
`docs/decisions/`; process in `docs/development/`; operations in
`docs/operations/`; reference material in `docs/reference/`; security
detail in `docs/security/`. `README.md` is the front door and stays
concise; normative detail is never duplicated into it.

The wiki is generated surface, seeded and updated from `docs/wiki/` by
`repoforge wiki sync`. Its guarantees:

- the default run is a dry run that writes and pushes nothing;
- pages outside the declared set are user-owned: reported, never
  modified, never deleted;
- the publish path verifies the clone's origin against the requested
  repository before any write;
- pushes are never forced, so a wiki that advanced since the clone stops
  the sync and the operator re-plans;
- a missing or disabled wiki is reported as a configuration problem and
  an authentication failure as a credential problem, because the two
  have different remedies;
- a token is passed through the environment, never a URL or a file.

Every generated repository ships the same arrangement: `docs/wiki/`
holds the seed pages, and the README says the pages are generated and a
hand edit is a bug.

`has_issues: true` and `has_wiki: true` are required by the governance
standard: a policy that declares a governance profile and disables
either is rejected at load time, and every governance profile document
is validated to require both.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Wiki as the source of truth | No review, no branches, no CI; content rots invisibly. |
| No wiki at all | Loses the browsable entry point that new contributors actually use. |
| Force-push the wiki to guarantee a clean state | Destroys any hand-written page the project values. |
| A hand-maintained wiki alongside generated pages | Exactly the second source of truth this decision exists to prevent. |

## Consequences

- A wiki page is a summary; when it disagrees with the repository, the
  repository wins and the page is a bug with a fix in `docs/wiki/`.
- Wiki setup requires the repository to have its wiki enabled, which is
  now part of the standard and validated.
- `wiki sync` needs git and a credential for the push; planning needs
  neither.

## Security, maintainability, and operational implications

- No credential is required for the default (planning) mode.
- A diverted or hijacked wiki destination is refused before a write.
- Operationally, drift between the repository and the wiki is visible
  as a non-empty plan and is closed by running sync.

## Links

- Implementation: `internal/wiki/wiki.go`, `internal/wiki/git.go`
- Seed content: `template/docs/wiki/`
- Tests: `internal/wiki/wiki_test.go`, `internal/config/governance_test.go`
