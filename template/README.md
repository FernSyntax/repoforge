# {{ .ProjectName }}

{{ .Description }}

## What RepoForge provides

This repository was generated from the RepoForge template. The
standard gives you:

- A baseline of community files (license, conduct, security, support,
  contributing, changelog).
- CI workflows: repository verification, OpenSSF Scorecard analysis,
  and a provenance gate that rebuilds this repository from the
  RepoForge commit recorded in `repoforge.manifest.json`. All actions
  pinned to commit SHAs.
- Deliberately **no** Dependabot configuration: the generated workflow
  pins are owned by the engine (`template/`), where dependency bumps
  land and reach this repository through the publication pipeline. Add
  your own `.github/dependabot.yml` once your project has dependencies
  to review — after adoption it will work, because adoption releases
  the provenance gate.
- Governance as code: `repoforge bootstrap` converges this
  repository's GitHub settings (merge methods, rulesets protecting
  `main` and release tags, Dependabot, secret scanning) to the policy
  in `.repoforge/config.yml`; `repoforge doctor` audits drift
  read-only; `repoforge validate` checks the local checkout
  deterministically.
- An ownership model (`.repoforge/ownership.yml`) declaring which
  paths tooling manages, which it scaffolds once, and which are yours.

## Start here

- [spec.md](spec.md) — the project's normative specification. Fill it in
  before the first feature: every issue, pull request, and test points
  back to a requirement ID in it.
- [docs/development/first-day.md](docs/development/first-day.md) — the
  full adoption walkthrough.
- [docs/decisions/](docs/decisions/) — where significant decisions are
  recorded, with a ready-to-copy template.

## The wiki

GitHub Wiki is seeded from `docs/wiki/` in this repository:

```sh
repoforge wiki sync --repo {{ .Owner }}/{{ .ProjectName }}              # preview
repoforge wiki sync --repo {{ .Owner }}/{{ .ProjectName }} --apply      # publish
```

Preview first. Sync never deletes a page it does not declare and never
force-pushes. See
[docs/operations/wiki-maintenance.md](docs/operations/wiki-maintenance.md).

## What you own

Everything not listed under `managed_paths` in
`.repoforge/ownership.yml`. RepoForge tooling never creates,
modifies, or deletes files in `user_owned_paths`, never overwrites
unmanaged rulesets, and never changes repository visibility.

## Fresh repository, fresh history

A repository generated from a GitHub template starts with **the
template's file contents and a brand-new, unrelated Git history** —
one initial commit, no shared history with the template, no template
commits in `git log`. The template's history is not your project's
history, and template history/blame is unavailable to you by design.

## First run

See [docs/development/first-day.md](docs/development/first-day.md) for
the full walkthrough. Short version:

```sh
gh repo create {{ .Owner }}/{{ .ProjectName }} --template {{ .Owner }}/repoforge-template --public --clone
cd {{ .ProjectName }}
repoforge bootstrap   # review the plan, then confirm
repoforge doctor      # expect all PASS
```

Then: first issue → first branch → first commit → first PR.

## Required customization

Before the first PR, adopt the repository — resolve every
double-brace placeholder with real values, explicitly and
reviewably:

```sh
repoforge template adopt \
  --var ProjectName=<name> \
  --var Owner=<owner> \
  --var RepoURL=<url> \
  --var Description=<description> \
  --var StandardVersion=v1
```

Add `--dry-run` first to preview every substitution without
writing; re-running with the same values is a no-op. Then review
`.repoforge/config.yml` (`visibility`, `governance_profile`,
`project.kind`). `repoforge validate` fails while placeholders
remain — that is deliberate.

## Git workflow

Trunk-based: `main` is the only permanent branch; topic branches named
`<type>/<issue>-<description>`; squash merge only; the source branch
is deleted on merge. Details:
[docs/development/branch-strategy.md](docs/development/branch-strategy.md).

## Commit workflow

Conventional Commits (`<type>(<scope>): <subject>`); atomic, coherent,
reviewable commits; the PR title becomes the squash commit subject.
Details:
[docs/development/commit-strategy.md](docs/development/commit-strategy.md).

## Pull request workflow

One coherent purpose per PR; the provided template's sections are
mandatory (what, why, validation, risk, operational impact,
breaking/migration); CI must be green. Details:
[docs/development/pull-request-strategy.md](docs/development/pull-request-strategy.md).

## Release workflow

Semantic Versioning, annotated `vMAJOR.MINOR.PATCH` tags on `main`, a
changelog section and a GitHub release per tag. Details:
[docs/development/release-strategy.md](docs/development/release-strategy.md).

## Security expectations

- Never commit credentials or tokens; secret scanning with push
  protection is enabled.
- Report vulnerabilities privately per [SECURITY.md](SECURITY.md).
- Dependency review runs on every PR; keep dependencies intentional.

## Removing RepoForge deliberately

Opting out is allowed — silently is not. Do it in a reviewed PR:

1. Delete `.repoforge/` and any RepoForge-managed workflows you
   do not want.
2. Remove the `repoforge:main` and `repoforge:release-tags`
   rulesets in the repository's GitHub settings.
3. State the decision in the PR description so the change is auditable.

## License

[Apache-2.0](LICENSE)
