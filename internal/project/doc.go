// Package project loads and validates the RepoForge project model
// (spec.md §24, FR-031): the typed, versioned, structured form of an
// approved user project. Loading is strict: unknown fields and unknown
// schema versions are rejected, and every field is validated
// semantically. Model output is validated, never trusted.
package project
