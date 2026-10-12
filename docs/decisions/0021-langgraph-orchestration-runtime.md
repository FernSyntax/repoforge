# ADR-0021: LangGraph orchestration runtime alongside the Go runner

- Status: accepted
- Date: 2026-10-12
- Spec: spec.md §24, FR-036 (planned)
- Implementation: `orchestration/*`
- Implements: issue #48
- Supersedes: 0018 (Go-native staged orchestration)

## Context

ADR-0018 chose a Go-native runner and deferred LangGraph: no Python
existed in the repository and the cost exceeded the vertical slice.
The product mandate since requires the LangGraph journey explicitly —
intake questions, decision-model routing into the MCP server,
model-generated copy, approval interrupts, and persistent checkpoints
— and the owner directed building it. The Go runner (`internal/
orchestrate`, merged) stays for deterministic stages; the graph owns
the agentic journey. Both share one contract: validated project
documents in, engine operations (CLI/MCP) as tools, bounded repair,
human approval for consequential actions.

## Decision

A Python LangGraph runtime lives in `orchestration/`: pinned,
hash-verified dependencies (`langgraph==1.2.14`,
`langgraph-checkpoint-sqlite==3.1.1`, `pytest==9.1.1`, Python ≥3.11),
a ~10-node graph (intake → spec → approval interrupt → project model
→ MCP-stdio generate → provider content pass → verify → bounded
repair → report), SQLite checkpoints per thread, gateway calls over
plain HTTPS with the unified key from env, and the LLM call behind an
injectable function so tests run offline. The graph drives the
engine through its real contracts (MCP stdio server, CLI) — never a
second implementation. Staged autonomy stays bounded: attempt caps,
no hundred-agent swarms (free-tier math and reviewability forbid
them), interrupts before generation and publication.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Keep ADR-0018 (Go only) | Directly contradicts the mandated LangGraph journey the owner ordered built; supersession is the honest path. |
| langchain_openai / agent SDKs for inference | Heavier dependency surface for an OpenAI-compatible POST the stdlib-equivalent already expresses; the provider abstraction stays in Go. |
| Unpinned or unhashed Python deps | Supply-chain posture (Scorecard pinned-dependencies) requires exact pins with hashes; `requirements.txt` carries both. |
| Hundreds of specialized agents | Quota exhaustion on free tiers mid-run plus unreviewable diffs; ~10 explicit nodes with caps deliver the same journey verifiably. |

## Consequences

Two runtimes to maintain, a Python CI job, and cross-language
contracts to keep honest — the price of the mandated journey. What
improves: interrupts, graph observability, and the LangGraph
ecosystem plug in directly; the Go runner keeps serving
deterministic stages untouched.

## Security, maintainability, and operational implications

- Security: gateway key from env only, redacted in logs; checkpoints
  carry briefs and digests, never credentials; MCP calls stay inside
  the server's allowed root; interrupts gate generation and any
  external publication.
- Maintainability: one graph file owns nodes and edges; maintainers
  must know LangGraph interrupts/checkpointers and the MCP tool
  contracts. Python ≥3.11, venv, hashed installs.
- Operations: `run.py` starts/resumes threads; SQLite file per
  workspace; retention is operator policy.

## Links

- Issue: #48
- Pull request: #49
- Tests: `orchestration/tests/test_graph.py` (stubbed LLM, temp dirs, no network)
- Related decisions: 0018 (superseded), 0016 (provider boundary), 0017 (MCP contract)
