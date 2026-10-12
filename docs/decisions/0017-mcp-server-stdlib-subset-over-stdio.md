# ADR-0017: MCP server as a stdlib JSON-RPC subset over stdio

- Status: proposed
- Date: 2026-10-12
- Spec: spec.md §24, FR-035 (planned)
- Implementation: `internal/mcp/*`, `repoforge mcp serve`
- Implements: issue #36

## Context

FR-035 needs a narrow MCP tool surface (capabilities, project
validation, template build into an approved directory, template
verification) with validated arguments and filesystem boundaries. The
current MCP protocol version is `2025-11-25`, verified 2026-10-12
against the official Go SDK (`github.com/modelcontextprotocol/go-sdk`
v1.8.0) sources. P11 bars unjustified dependencies, and only four
tools are needed — no resources, prompts, or sampling.

## Decision

The engine speaks the MCP client-needed subset directly with the Go
standard library: JSON-RPC 2.0, newline-delimited, over stdio —
`initialize`, `notifications/initialized`, `ping`, `tools/list`,
`tools/call`, with standard error codes. The server declares protocol
`2025-11-25`. Tools wrap existing engine contracts instead of
reimplementing them (`template.Build`/`Verify`, `project` loading and
validation). The server is configured with `--allowed-dir`; every
writable path resolves inside it through symlink evaluation before
any decision, on top of the engine's own output policy. No remote
transport is implemented.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Official MCP Go SDK v1.8.0 | Pulls JWT, JSON-schema, and URI-template libraries (20-line go.mod) for a four-tool stdio server that uses none of them; license in MIT→Apache-2.0 transition adds review burden; P11 not satisfied. Revisit if the surface grows to resources/prompts/sampling. |
| Generic exec tool passing arbitrary CLI strings | A huge unvalidatable tool defeats FR-035's precise schemas and least privilege; rejected by the requirement itself. |
| Remote (SSE/streamable-HTTP) transport | No v0.2.0 consumer needs it; a network listener widens the attack surface for architectural appearance. |
| Second generation engine inside MCP | Duplicates `internal/template` business rules without conformance tests; the tools call the engine instead. |

## Consequences

Four auditable tools with zero new dependencies and protocol tests
against recorded shapes. What hurts: the engine owns MCP wire
compatibility for the subset it speaks — a protocol change means
engine code — and clients expecting resources/prompts get
method-not-found until the surface grows by decision, not drift.

## Security, maintainability, and operational implications

- Security: stdio only (no listener); two-layer filesystem boundary
  (server root containment plus engine output policy); arguments are
  schema-checked before any engine call; no credentials involved.
- Maintainability: one package owns the subset; maintainers must know
  the four tool contracts and the pinned protocol version, both in
  the package doc.
- Operations: `repoforge mcp serve --allowed-dir DIR` runs in the
  foreground until stdin closes; every request is independent and
  stateless.

## Links

- Issue: #36
- Pull request: TBD
- Tests: `internal/mcp/*_test.go` (in-process stdio sessions)
- Related decisions: 0001 (Go CLI, stdlib-first), 0016 (provider boundary style)
