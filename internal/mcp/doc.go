// Package mcp exposes four deterministic engine operations over the
// Model Context Protocol client-needed subset (ADR-0017, spec.md §24
// FR-035): JSON-RPC 2.0, newline-delimited, over stdio, declaring
// protocol version 2025-11-25 (verified against the official Go SDK
// v1.8.0 sources; no SDK dependency).
//
// The tools wrap existing engine contracts instead of reimplementing
// them: capabilities reports versions, project_validate loads and
// validates a project document (internal/project), template_build
// renders the canonical template source, product_build renders the
// Astro product source (both internal/template), and template_verify
// checks a built tree. Writable paths resolve inside
// the server's allowed root through symlink evaluation before any
// decision, on top of the engine's own output policy. No remote
// transport is implemented.
package mcp
