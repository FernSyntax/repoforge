# ADR-0016: Provider abstraction over the OpenAI-compatible gateway contract

- Status: accepted
- Date: 2026-10-12
- Spec: spec.md §24, FR-032, FR-033, FR-034, FR-038 (planned)
- Implementation: `internal/provider/*`
- Implements: issue #33

## Context

Orchestration needs model inference, but application code must not
hard-code model IDs, assume capabilities, or depend on one gateway's
SDK. The verified FreeLLMAPI deployment exposes an OpenAI-compatible
surface (`POST /v1/chat/completions` with Bearer unified key,
`model: "auto"` router entries, `GET /v1/models?execution_status=ready`
for availability, clear 400/422/429/5xx errors; verified 2026-10-12
against the deployed `docs/en/api/01-rest-api.md`). P11 bars new
dependencies without justification, and NFR-007 requires mocked tests
with no live network in CI.

## Decision

The engine speaks the OpenAI-compatible HTTP contract directly with
the Go standard library (`net/http`, `encoding/json`): no provider
SDK. `internal/provider` owns a logical-task routing table (task →
model route, default `auto`), runtime availability checks against the
models endpoint (never assumed capabilities), free-only failover
across configured routes with pause-on-exhaustion (never paid
fallback), bounded classified retries with `Retry-After` respected,
and env-only credentials (`FREELLMAPI_KEY`, `FREELLMAPI_BASE_URL`)
redacted from every error. Gateway specifics stay in configuration;
capability facts stay discovered at runtime.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Official OpenAI Go SDK dependency | New third-party dependency for wire shapes the stdlib already expresses; P11 requires justification the SDK does not earn, and it bakes in one vendor's types. |
| Python LangChain/LangGraph client for inference | Splits the runtime before the orchestration boundary is decided (that is ADR-0017's question); the provider must serve Go callers (MCP, CLI) anyway. |
| Hard-coded model IDs per task | Availability and free-tier access change weekly; hard-coding guarantees silent breakage and violates FR-032. |
| Treating the gateway as always-available | 429/5xx/outage paths are the normal case on free tiers; FR-033/FR-034 require explicit exhaustion and retry behavior. |

## Consequences

Inference calls become uniform, mocked-testable Go code with no new
module dependencies. What hurts: the engine owns wire compatibility,
so a gateway contract change means engine code changes — contained by
recording the verified documentation revision above and testing
against recorded shapes. Task-to-model routing lives in one table,
which reviewers must keep small.

## Security, maintainability, and operational implications

- Security: the unified key travels only in the `Authorization` header
  from process env, never in logs, errors, fixtures, or URLs; error
  bodies are redacted before embedding. Outbound context stays
  caller-supplied (no repository-wide exfiltration by default).
- Maintainability: one package owns the contract; maintainers must
  know the OpenAI chat-completions shape and the gateway's documented
  error semantics, both referenced from the package doc.
- Operations: default base URL targets a local gateway
  (`http://localhost:3001/v1`); remote operation is explicit config.
  No live gateway is required for tests or CI.

## Links

- Issue: #33
- Pull request: #35
- Tests: `internal/provider/*_test.go` (mocked gateway via `httptest`)
- Related decisions: 0001 (Go CLI, stdlib-first), 0002 (schema discipline, applied to route config)
