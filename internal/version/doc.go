// Package version exposes the build version of the repoforge CLI.
// The values are stamped by release tooling; see spec.md, "Release
// model".
package version

// Version is the CLI build version. Release builds override it via
// -ldflags "-X .../internal/version.Version=<tag>".
var Version = "0.0.0-dev"

// Commit is the full git commit SHA the binary was built from,
// stamped via -ldflags "-X .../internal/version.Commit=<sha>".
// Empty for local development builds.
var Commit = ""

// Date is the build date (UTC, RFC 3339). Release builds stamp the
// commit date — source-derived, so builds stay reproducible — via
// -ldflags "-X .../internal/version.Date=<date>".
var Date = ""
