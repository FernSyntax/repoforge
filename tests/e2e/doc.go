// Package e2e is the explicit end-to-end harness for disposable GitHub
// repositories. It exercises the full product loop — create from
// template, govern, drift, reconcile, PR flow, merge, cleanup —
// against the live GitHub API.
//
// It never runs in normal CI: every test skips unless
// REPOFORGE_E2E=1, and REPOFORGE_E2E_OWNER,
// REPOFORGE_E2E_TEMPLATE (owner/name), and a token (GH_TOKEN or
// GITHUB_TOKEN) must be set. The created repository is always
// <owner>/repoforge-e2e-<timestamp>; anything else is refused, so
// neither repoforge itself nor any real repository can be
// targeted. Created resources are deleted on success and on failure
// (best-effort, logged). Explicit runs need a long timeout
// (e.g. -timeout 30m): CI polling dominates.
//
// Prerequisites on PATH: git, gh (authenticated).
//
// Two preconditions are enforced before anything is created, so a
// run can neither orphan resources nor chase ghosts: the token must
// provably carry repo deletion (else fail fast with the re-auth
// remedy), and the harness waits for the template-generated initial
// commit to exist (creation returns before GitHub generates it).
package e2e
