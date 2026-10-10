# Wiki Maintenance

The GitHub wiki is generated surface. `docs/wiki/` in this repository is
the source of truth, and `repoforge wiki sync` converges the hosted wiki
to it (spec.md §13.9, ADR-0010).

## Seeding a wiki

1. Confirm the repository has its wiki enabled (`has_wiki: true`, which
   the governance standard requires).
2. Author the pages as Markdown files in `docs/wiki/`, one page per
   file, named after the page: `docs/wiki/Home.md` becomes the page
   `Home`.
3. Preview:

   ```sh
   repoforge wiki sync --repo FernSyntax/repoforge
   ```

   This writes nothing and pushes nothing.
4. Publish:

   ```sh
   repoforge wiki sync --repo FernSyntax/repoforge --apply
   ```

## What sync does and does not do

| Behaviour | Guarantee |
| --- | --- |
| Default run | Dry run: prints the plan, writes nothing, pushes nothing. |
| Declared pages | Created when absent, updated when the content differs. |
| Undeclared pages | User-owned: reported, never modified, never deleted. |
| GitHub's own `_Sidebar.md` / `_Footer.md` | Never read, never rewritten. |
| Push | Never forced. A rejected push means the wiki advanced; re-run to re-plan. |
| Destination | The clone's origin is verified against the requested repository before any write. |
| Credentials | Passed to git through the environment, never a URL, a file, or an argument. |

## Failure modes

| Symptom | Meaning | Remedy |
| --- | --- | --- |
| "does not exist or its wiki is disabled" | A configuration problem, not a credential problem. | Enable the wiki on the repository, then re-run. |
| "authentication failed" | A credential problem. | Check the token and that the wiki is enabled. |
| "refusing to publish to a different repository" | The clone's origin is not the requested repository. | Fix the `--repo` value or the remote; nothing was written. |
| "pushing was rejected" | The wiki advanced since the clone. | Re-run: the plan is recomputed from current state. |

## Drift

A wiki page is a summary. When a page and the repository disagree, the
repository wins and the page is a bug: fix `docs/wiki/<page>.md`, open a
pull request, and re-run sync. A hand edit on the wiki is lost at the
next sync and hides the real fix, so it is never the remedy.

## Credentials

Planning needs no credential at all. Publishing needs one that can write
the wiki, resolved from `GH_TOKEN`, `GITHUB_TOKEN`, or the `gh` session
(spec.md §8.1). The token is never persisted, logged, or interpolated
into an error message.
