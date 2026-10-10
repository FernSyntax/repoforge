// Package bootstrap converges a repository to the RepoForge policy.
//
// Bootstrap is idempotent (spec.md §10.2): running it twice converges to the
// same state and reports zero changes on the second run. It follows the
// plan -> confirm -> apply -> re-read -> verify cycle (spec.md §10.3) and
// never mutates during planning. Dry-run prints the complete plan and
// performs no local or remote mutation (spec.md §10.4). On failure it stops,
// reports what was applied and what remains, and leaves the repository
// in a state a re-run converges from (spec.md §10.6). See spec.md,
// "Bootstrap model".
package bootstrap
