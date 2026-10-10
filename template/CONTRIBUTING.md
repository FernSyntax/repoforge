# Contributing

This repository follows the RepoForge engineering standard.

## Ground rules

- One coherent purpose per pull request.
- Major design decisions require an ADR in `docs/decisions/` before
  implementation.
- CI is authoritative. Run `sh scripts/verify` locally for fast
  feedback.
- Never commit credentials, tokens, or machine-specific paths.

## Workflow

- Branch from `main` as `<type>/<issue>-<description>` where type is
  one of: feat, fix, refactor, perf, test, docs, build, ci, security,
  chore.
- Commit messages follow Conventional Commits:
  `<type>(<scope>): <subject>`. The pull request title becomes the
  squash-merge commit subject.
- Pull requests use the provided template: what changed, why,
  validation, risk, operational impact, breaking/migration impact.
- Address review feedback with new commits; do not force-push a branch
  under review.

## Security

Report vulnerabilities privately per [SECURITY.md](SECURITY.md). Never
file security issues publicly.
