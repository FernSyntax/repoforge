# AGENTS.md

Operating contract for AI coding agents working in this repository —
human or artificial, any platform. Read this file and
[DESIGN.md](DESIGN.md) before your first task; they are the only two
files that teach you how to work here.

Interoperability notes, not the source of project truth. If this file
and [spec.md](spec.md) disagree, spec.md wins; this file is the bug —
fix it.

## Truth hierarchy

1. [spec.md](spec.md) — normative product specification. Wins every
   disagreement.
2. [docs/decisions/](docs/decisions/) — accepted ADRs. Design history
   with reasons; [DESIGN.md](DESIGN.md) is the index to the reasoning.
3. [docs/development/](docs/development/) — normative process
   (branching, commits, PRs, hooks, releases). Extensions of spec.md.
4. This file — how to operate. Points at the above; duplicates none
   of it.

## First session

1. Read [DESIGN.md](DESIGN.md) for what the system is and where
   design lives.
2. Read the workflow doc for what you are about to touch
   ([git-workflow.md](docs/development/git-workflow.md) is the index).
3. Major design decisions require specification text or an ADR in
   `docs/decisions/` **before** implementation — no exceptions, no
   "implement now, document later".

## Verification

Run before declaring any task complete, and report failures honestly:

```sh
gofmt -l .
go vet ./...
go test ./...
git diff --check
```

`repoforge validate --path .` checks the checkout against the
standard (files, links, ADRs, conventions). CI is authoritative;
local runs are feedback only.

## Mandatory workflows

Issue-first, always: a change without a linked issue is not a change.
Branch as `<type>/<issue>-<description>` (`feat/7-...`, never bare
words); commit with Conventional Commits (the title becomes the
squash-merge subject); one coherent purpose per pull request.

```sh
# Issues and PRs: scaffold, fill, gate — never freehand.
repoforge issue scaffold --type bug|feature|task|security
repoforge issue check --type T --file BODY        # or create directly:
repoforge issue create --type T --title T --file BODY [--repo slug]
repoforge pr scaffold --issue N
repoforge pr ready --file BODY [--repo owner/name]  # green means go
# Or gate-and-delegate in one step (gh owns auth):
repoforge pr create --title T --file BODY [--repo slug]
```

Releases: `repoforge release ready --tag vX.Y.Z` before tagging.
Tagging, pushing, and publishing are external side effects — the
check proves the tag belongs; the push fires it. Never create a tag
or release to "test" the pipeline.

Hooks: `repoforge hook install` puts the blocking hooks in
(`commit-msg`, `pre-push`, `pre-commit`). They interrupt you before
CI does. Bypassing with `--no-verify` is visible and never excuses
a red run.

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
- Comments explain why, not what. No stale, misleading, or
  paraphrasing comments. A `TODO` without a linked issue is a defect.
- Stop when the requested scope is complete. Report what changed and
  how it was verified.

## Workflow docs

Branching, commits, PRs, releases, hooks: `docs/development/`
(git-workflow.md is the index). Design authority: [DESIGN.md](DESIGN.md).
These are normative extensions of spec.md — consult them before
touching those areas.
