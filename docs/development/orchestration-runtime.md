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
| `graph.py` | The journey: intake → spec_draft → spec_critic → approve_gate → design → project_model → generate → design_apply → content → review → verify → report. |
| `agents.py` | The review mesh: read-only accessibility, SEO/AEO, security (blocking), performance/privacy, and i18n auditors plus the advisory spec critique. |
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

## Production layer

One command takes a Stitch prototype to a production site:

```sh
repoforge generate -dir /run --brief brief.json -yes \
  --domain example.com
```

What the journey adds on top of the faithful Stitch page:

| Area | What ships |
| --- | --- |
| Themes | Stitch theme is the dark design; a light palette is **derived** from it, every text/surface pair forced to WCAG AA (4.5:1) with automatic correction, plus a persistent dark/light toggle (`localStorage`, honors `prefers-color-scheme`). |
| SEO | `<title>`, capped meta description, canonical, Open Graph + Twitter cards, `og.svg` social image, `sitemap.xml`, `robots.txt` with the sitemap reference. |
| AEO/GEO | WebSite + Organization + FAQPage JSON-LD, question headings, `llms.txt` map, landmarks, locale signals. |
| Security | Hash-pinned Content-Security-Policy in `public/_headers`, HSTS, frame/type/referer/permissions headers, external links hardened with `rel=noopener`, no `javascript:` URLs — link and secret findings **block** verify. |
| Offline | Tailwind compiled to static CSS, fonts self-hosted: zero remote origins at view time. |
| Review | A11y, SEO/AEO/GEO, images, performance, i18n, ICP fit, security, links. Advisory findings warn; security and links fail the run. |

`--domain` is required for absolute URLs (canonical, sitemap,
og:image). Without it the site stays relative — no host is ever
invented. ICP fit scores brief vocabulary against the shipped copy;
fabrication flags (money, percentages, metrics) are reported for human
review, because Stitch body copy invents them.

## Stitch design input (optional)

When `STITCH_API_KEY` is set, the journey takes a bounded design
branch after approval: one Stitch project, one desktop screen from
the brief, plus its tokens and screenshot references, all recorded in
run state and fed to the content pass as direction. A second node
(`design_apply`, after generation) downloads the screen HTML and
applies the design — theme tokens into `global.css`, Stitch nav
structure into the header/footer rewired to real in-page anchors —
while generated copy stays brief-driven: Stitch text routinely invents
statistics, certifications, and URLs, so claim-shaped labels are
dropped with reasons in the report and the review mesh validates the
result. Faithful mode goes one step further: the generated index page
IS the sanitized Stitch screen (Tailwind loader + theme config kept
verbatim via `is:inline`, event handlers and non-Tailwind scripts
dropped, `<html>` theme class preserved), so the site looks like the
Stitch design. Body copy in faithful mode is Stitch-generated and the
report requires human review before publishing; the content pass
releases its FAQ contract and verify checks structure instead.
`design_apply` then bakes the theme to static CSS (`tailwindcss@3`
plus the CDN plugins the document asked for, dev-only, workspace
cleaned afterwards) and self-hosts the Google Fonts into
`public/fonts`, so the page renders identically with no CDN or font
dependence at view time. Any bake step may fail back to the faithful
CDN page with a recorded reason. Bounds: one project and one screen per run, reference HTML
capped at 2MiB, no automatic deletion of remote state, failures skip
the branch with a recorded reason instead of failing the run. Without
a key the branch records `design_skipped` and the journey continues on
the local template.

```sh
export STITCH_API_KEY="<rotated-key>"   # env only; never commit, never paste
```

Key hygiene is mandatory: a key that was ever pasted anywhere is
compromised — rotate it in console immediately, restrict it, and keep
it out of logs, prompts, checkpoints, and the repository. The client
sends it only as the `X-Goog-Api-Key` header to
`https://stitch.googleapis.com/mcp` and redacts it from every error.

## Visual audit (optional, advisory)

After the content pass, the journey reviews at most one Stitch render
through a vision-capable gateway route: contrast, hierarchy, spacing,
legibility, and focus as structured JSON findings (`info`/`minor`/
`major`). Majors are recorded as release follow-ups in the final
report; nothing here blocks the run, and missing screenshots, fetch
failures, model outages, or invalid output all resolve to a recorded
skip. Bytes are fetched with the Stitch key header, capped at 4 MiB,
and never logged. Without screenshots the stage is a no-op.

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
