// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

// Package config loads and validates the declarative RepoForge
// repository policy. Loading is strict (spec.md §9.2): unknown fields,
// trailing content, and unknown schema versions are all rejected, and
// defaults follow the schema (spec.md §9.4). See spec.md, "Configuration
// model".
package config
