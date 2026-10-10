# Maintainer Workflow

How work lands in the engine repository. This is the loop every
change follows — no exceptions, no shortcuts.

## The loop

```
issue → branch → implement → verify → commit → push → PR → CI green → squash-merge → next
```

1. **Issue first.** Every change starts from an issue; branch names
   carry its number (`<type>/<issue>-<description>`, see
   `branching.md`).
2. **Branch** off updated `main`. One branch, one coherent purpose.
3. **Implement** only the requested scope. Read spec.md before
   architectural changes; spec text or ADR before design decisions.
4. **Verify locally before every push** — CI minutes are expensive,
   local checks are free:
   ```sh
   gofmt -l .
   go vet ./...
   go test ./...
   git diff --check
   go run ./cmd/repoforge validate
   ```
   Plus `actionlint`/`shellcheck` for workflow/script changes and
   `act` for workflow changes. Never push to find out what local
   checks already know.
5. **Commit** with Conventional Commits (see `commit-conventions.md`).
   Keep PR titles well under 72 characters — GitHub appends ` (#N)`
   on squash and the length gate counts it.
6. **Push** the branch, **open the PR** (body closes the issue),
   **wait for CI**, inspect failures honestly, fix on the branch.
7. **Squash-merge** only when every required check is green — then
   start the next branch. Never merge red, never bypass, never
   self-approve where review is required.

## Non-negotiables

- No commits without explicit instruction carrying the message.
- No pushes, merges, releases, or tags unless explicitly requested.
- No external GitHub mutations except the ones the task requests.
- No force-push to `main`, no disabling checks, no amending
  published history.
- Report verification honestly, including failures.
- Stop when the requested scope is complete.
