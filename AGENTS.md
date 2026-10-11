# AGENTS.md

Interoperability notes for AI coding agents. This file is not the source
of project truth — [spec.md](spec.md) is. If this file and spec.md
disagree, spec.md wins; fix this file.

## Required reading

Read [spec.md](spec.md) before any architectural change. Major design
decisions require specification text or an ADR in `docs/decisions/`
before implementation.

## Verification

Run before declaring any task complete, and report failures honestly:

```sh
gofmt -l .
go vet ./...
go test ./...
git diff --check
```

CI is authoritative; these are local feedback only.

## Rules

- Keep changes scoped to the task. Do not implement future phases early.
- Do not commit unless the task supplies the commit message. Do not push
  unless explicitly requested.
- Do not perform external GitHub mutations unless the task explicitly
  requests them. Prefer dry-run/read-only paths.
- Never store credentials or print tokens. Never hardcode users, orgs,
  repository names, emails, or ownership values. Files may contain Go
  template placeholders (e.g. `{{ .Owner }}`) resolved by the template
  builder/bootstrap — do not "fix" them by substituting identities.
- Never bypass repository governance: no force-push, no disabling
  checks, no self-approving, no amending published history.
- Before opening a pull request, scaffold its body with
  `repoforge pr scaffold --issue N` and do not open it until
  `repoforge pr ready --file <body> [--repo owner/name]` reports OK.
  The same applies to issues via `repoforge issue scaffold` and
  `repoforge issue check`. A red CI check you could have run locally
  is an authoring failure, not a CI failure.
- Stop when the requested scope is complete. Report what changed and
  how it was verified.

## Workflow docs

Branching, commits, PRs, and releases: `docs/development/`
(git-workflow.md is the index). These are normative extensions of
spec.md — consult them before touching those areas.
