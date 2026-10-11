// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package validate checks a repository against the RepoForge policy
// deterministically: the same inputs must always produce the same
// result (spec.md §12.2). It is local and offline: no network, clock,
// randomness, or machine-specific state influences the verdict
// (spec.md §12.3). Findings are printed in a stable order and exit codes are
// 0 = pass, 1 = violation, 2 = tooling error (spec.md §12.5). See spec.md,
// "Validation model".
package validate
