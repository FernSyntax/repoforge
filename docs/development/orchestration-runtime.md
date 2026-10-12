# Orchestration runtime

The LangGraph journey (ADR-0021): from a client brief to a verified,
built marketing site, with human interrupts, SQLite checkpoints, and
bounded repair. Deterministic Go work stays in the engine; this
runtime owns the agentic steps.

## Layout

| File | Role |
| --- | --- |
| `brief.py` | The intake questions (ICP, audience, tone, keywords, geography, non-goals, success metric). |
| `state.py` | `RunState` — the resumable contract; digests, never credentials. |
| `graph.py` | The journey: intake → spec_draft → approve_gate → project_model → generate → content → verify → report. |
| `llm.py` | Gateway caller: OpenAI-compatible, Bearer key from env, classified failures. |
| `mcp_client.py` | Drives `repoforge mcp serve` over stdio. |
| `run.py` | start / resume / status CLI. |

## Local setup

Requires Python ≥3.11 and `uv` (or pip):

```sh
uv venv --python 3.12 orchestration/.venv
uv pip install --python orchestration/.venv/bin/python \
  --require-hashes -r orchestration/requirements.txt
```

The requirements file is compiled with hashes (Scorecard
pinned-dependencies): 45 transitive packages, exact pins.

## Running a journey

Answer the brief in a JSON file (all keys required; see
`brief.py` for what each means):

```json
{
  "project_name": "acme-site",
  "purpose": "One sentence about what the site is for.",
  "ideal_customer_profile": "Who pays and why they care.",
  "target_audience": "Secondary readers.",
  "tone_of_voice": "Plain and direct.",
  "primary_keywords": "acme, demo",
  "geographic_scope": "Worldwide.",
  "non_goals": "No invented statistics.",
  "success_metric": "Completed demo requests."
}
```

```sh
export FREELLMAPI_KEY=<unified-gateway-key>      # never commit
~/.local/bin/repoforge --version                  # binary on PATH
python orchestration/run.py start \
  --workdir /tmp/opencode/site-run --brief brief.json
```

The graph pauses at the approval gate and prints the spec digest;
answer with:

```json
{"approval": {"approved": true, "artifact": "<digest>", "by": "your-name"}}
```

```sh
python orchestration/run.py resume \
  --workdir /tmp/opencode/site-run --thread <run-id> \
  --payload approval.json
```

Exit codes: `0` done, `3` paused (resume), `2` usage/startup failure.
A failed run reports what failed, whether files changed, and how to
resume.

## Gateway routing

Task routes default to `auto` (the gateway router). Pin one with
`--route spec-drafting=gemini-2.5-flash`; routes never leave
configuration. If no eligible route answers, the run stops with a
classified `rate-limit` failure — there is no paid fallback.

## Testing

```sh
python -m pytest orchestration/tests -q
```

Fully offline: the LLM and MCP are fakes, so no network and no key
are required. The live-binary path (`REPOFFORGE_BIN` set) is marked
`integration` and runs the real `repoforge mcp serve`.

## Checkpoints and retention

Threads persist in `<workdir>/runs.sqlite3`. Checkpoints hold briefs,
digests, and status — never credentials. Delete the file to reset all
runs; that is the retention policy (operator-managed).

## What is deliberately not here

- No autonomous hundred-agent swarm: quota on free tiers and diff
  reviewability cap autonomy at these bounded nodes.
- No live publishing: deployment and release stay human actions.
