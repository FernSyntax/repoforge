// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package integration holds opt-in end-to-end tests against a real
// GitHub test repository. They never run in CI or in a normal
// `go test ./...`: every test skips unless REPOFORGE_TEST_REPO is
// set to an owner/name slug, and the suite refuses any slug that
// does not read as a throwaway (it must contain "test" — the
// engine's own repository name fails that check by construction).
//
// Read-only coverage (doctor, bootstrap dry-run) needs only the slug
// plus a token (GH_TOKEN or GITHUB_TOKEN). The converging test
// additionally requires REPOFORGE_TEST_APPLY=1 and a public test
// repository; it manages that repository's settings, rulesets,
// labels, and security features for real, twice, and asserts the
// second run is a no-op.
//
// Run explicitly, for example:
//
//	REPOFORGE_TEST_REPO=you/repoforge-smoke-test GH_TOKEN=... go test ./internal/integration/ -v
package integration
