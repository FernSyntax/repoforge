// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package doctor reports drift between a repository and the
// RepoForge policy. Doctor is strictly read-only: it must never
// modify the local repository or GitHub state (spec.md §11.2): it issues
// only GET requests, and a test asserts no non-GET path exists. Exit
// codes: 0 = no drift, 1 = drift found, 2 = the audit could not
// complete (spec.md §11.4). See spec.md, "Doctor model".
package doctor
