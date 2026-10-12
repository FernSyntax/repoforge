// Package acceptance proves the v0.2.0 vertical slice end to end
// (issue #44, spec.md §24.5 B1–B7): a valid project document drives an
// approved orchestration run to a verified, reproducible product
// tree, and an invalid document fails with a report.
//
// The test is offline and deterministic — no gateway, no
// credentials, no environment — so it runs in ordinary CI (NFR-007).
// Live systems stay behind tests/e2e (REPOFORGE_E2E=1); `npm run
// build` over the generated site stays operator verification,
// evidenced in the FR-039 PR body.
package acceptance
