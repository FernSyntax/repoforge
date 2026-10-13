"""Staged site-generation graph: the agentic journey in ~10 nodes.

intake → spec_draft → approve_gate → project_model → generate →
content → verify → (repair ⟳ | report). Human interrupts pause for
brief answers and for spec approval; checkpoints persist every step
in SQLite; every autonomous loop carries an attempt bound. The LLM
and MCP surface arrive via Deps so tests run fully offline.
"""

from __future__ import annotations

import hashlib
import json
import os
from dataclasses import dataclass

from langgraph.graph import StateGraph, START, END
from langgraph.types import interrupt

import brief as brief_mod
from state import RunState

MAX_APPROVAL_ROUNDS = 3
MAX_CONTENT_ATTEMPTS = 2
MAX_REPAIR_ROUNDS = 2
MAX_ANSWER_CHARS = 500
FAQ_PLACEHOLDER = "Replace this answer"
EXPECTED_FAQ_COUNT = 3


@dataclass
class Deps:
    """Injected boundaries. Tests substitute fakes; production wires
    the gateway caller and the MCP factory."""
    llm: object = None  # (task: str, messages: list) -> str
    mcp_factory: object = None  # () -> mcp_client.Client (context-managed by caller)
    stitch_factory: object = None  # () -> stitch.Client; None skips design
    allowed_root: str = ""
    site_rel: str = "site"
    max_content_attempts: int = MAX_CONTENT_ATTEMPTS
    max_approval_rounds: int = MAX_APPROVAL_ROUNDS
    max_repair_rounds: int = MAX_REPAIR_ROUNDS


def digest(text: str) -> str:
    return hashlib.sha256(text.encode()).hexdigest()


def _fail(state: RunState, message: str) -> dict:
    errors = list(state.get("errors", [])) + [message]
    return {"failed": True, "done": True, "stage": "failed",
            "errors": errors, "report": _report(state, message)}


def _report(state: RunState, failure: str = "") -> dict:
    lines = [
        f"run {state.get('run_id')} stage {state.get('stage')}",
        f"generated: {bool(state.get('generated'))}",
        f"verified: {bool(state.get('verified'))}",
        f"attempts: {state.get('attempts', {})}",
    ]
    if failure:
        lines += [f"failure: {failure}", "resumable: fix the cause and resume the thread"]
    else:
        lines += ["remaining: none", "action required: deploy dist/ from the generated site"]
    return {"report": "\n".join(lines)}


def _resource_name(envelope, kind: str) -> str:
    """First plausible resource identifier in a tool result, navigating
    defensively: Stitch nests resources differently per tool."""
    if not isinstance(envelope, dict):
        return ""
    for key in ("name", kind, "project", "screen", "asset"):
        value = envelope.get(key)
        if isinstance(value, str) and value:
            return value
        if isinstance(value, dict) and isinstance(value.get("name"), str):
            return value["name"]
    return ""


def _screenshot_urls(detail) -> list:
    try:
        from stitch import screenshot_urls as shots
        return shots(detail) if isinstance(detail, dict) else []
    except Exception:
        return []


def _design_tokens(detail) -> dict:
    """First token-like mapping in a screen asset, flattened one level
    so nested color groups stay usable."""
    if not isinstance(detail, dict):
        return {}
    for key in ("designSystem", "design_system", "tokens", "theme"):
        value = detail.get(key)
        if not isinstance(value, dict) or not value:
            continue
        flat = {}
        for sub_key, sub_value in list(value.items())[:8]:
            if isinstance(sub_value, (str, int, float)):
                flat[sub_key] = sub_value
            elif isinstance(sub_value, dict):
                for leaf_key, leaf in list(sub_value.items())[:4]:
                    if isinstance(leaf, (str, int, float)):
                        flat[f"{sub_key}.{leaf_key}"] = leaf
        return dict(list(flat.items())[:16])
    return {}


def design_stage(deps, state: RunState) -> dict:
    """One bounded Stitch pass: a project, one desktop screen, and its
    tokens plus screenshot references. Skips (never fails) without a
    client, on client errors, or on Stitch failures."""
    from stitch import StitchError, design_summary, screen_prompt
    factory = getattr(deps, "stitch_factory", None)
    if factory is None:
        return {"stage": "project_model",
                "design": {"skipped": "no-stitch-client"}}
    client = None
    try:
        client = factory()
    except Exception as e:
        return {"stage": "project_model",
                "design": {"skipped": f"client-unavailable: {e}"}}
    try:
        brief = state.get("brief", {})
        title = (f"{brief.get('project_name', 'site')}-"
                 f"{state.get('run_id', 'run')}")
        project = client.call_tool("create_project", {"title": title})
        project_name = _resource_name(project, "project")
        screen = client.call_tool("generate_screen_from_text", {
            "prompt": screen_prompt(brief),
            "deviceType": "DESKTOP",
        })
        record = {"project": project_name,
                  "screen": _resource_name(screen, "screen")}
        detail = {}
        if record["screen"]:
            try:
                detail = client.call_tool(
                    "get_screen", {"name": record["screen"]}) or {}
            except StitchError:
                detail = {}
        record["screenshots"] = _screenshot_urls(detail)
        record["tokens"] = _design_tokens(detail)
        record["summary"] = design_summary(record)
        return {"stage": "project_model", "design": record}
    except StitchError as e:
        return {"stage": "project_model",
                "design": {"skipped": f"stitch-failed: {e.kind}"}}
    finally:
        if client is not None:
            try:
                client.close()
            except Exception:
                pass


def build_graph(deps: Deps, checkpointer=None):
    """Compile the journey with injected dependencies."""

    def intake(state: RunState) -> dict:
        current = dict(state.get("brief", {}))
        missing = brief_mod.missing_answers(current)
        rounds = 0
        while missing:
            payload = {"questions": brief_mod.questions_for(missing)}
            answers = interrupt(payload)
            rounds += 1
            if isinstance(answers, dict):
                for k, v in answers.items():
                    if str(v or "").strip():
                        current[k] = v
            missing = brief_mod.missing_answers(current)
            if missing and rounds >= 3:
                return _fail(state, f"intake incomplete, still missing: {missing}")
        return {"brief": current, "stage": "spec_draft"}

    def spec_draft(state: RunState) -> dict:
        b = state["brief"]
        prompt = (
            "Draft a one-page site specification as markdown "
            "(purpose, audience, sections, acceptance criteria) from:\n"
            + json.dumps(b, indent=2, sort_keys=True)
        )
        for _ in range(2):
            spec = deps.llm("spec-drafting", [
                {"role": "system", "content": "You write concise site specifications."},
                {"role": "user", "content": prompt},
            ])
            if spec and spec.strip():
                return {"spec": spec, "spec_digest": digest(spec),
                        "stage": "approve_gate"}
        return _fail(state, "spec drafting returned empty output twice")

    def approve_gate(state: RunState) -> dict:
        rounds = int(state.get("approval_rounds", 0))
        while True:
            decision = interrupt({
                "action": "approve-spec",
                "artifact": state["spec_digest"],
                "spec": state["spec"],
            })
            rounds += 1
            if (isinstance(decision, dict) and decision.get("approved")
                    and decision.get("artifact") == state["spec_digest"]
                    and str(decision.get("by", "")).strip()):
                return {"approval": decision, "approval_rounds": rounds,
                        "stage": "project_model"}
            if rounds >= deps.max_approval_rounds:
                return _fail(state, "approval not granted within bounds")
            # Otherwise pause again with the mismatch noted.

    def design(state: RunState) -> dict:
        # Optional branch: real Stitch input when configured, a
        # recorded skip otherwise. Design failure never fails the run;
        # generation proceeds on the local template either way.
        return design_stage(deps, state)

    def project_model(state: RunState) -> dict:
        b = state["brief"]
        import re
        name = str(b.get("project_name", ""))
        if not re.match(r"^[a-z0-9][a-z0-9-]*$", name):
            return _fail(state, f"project name {name!r} is not filesystem-safe")
        doc = {
            "schemaVersion": "v1",
            "name": name,
            "purpose": str(b.get("purpose", "")),
            "users": [str(b.get("target_audience", ""))],
            "requirements": [
                {"id": "FR-001",
                 "text": f"Convert visitors toward {b.get('success_metric', '')}"},
                {"id": "FR-002", "text": str(b.get("purpose", ""))},
            ],
            "exclusions": [str(b.get("non_goals", ""))],
        }
        mcp = deps.mcp_factory()
        try:
            verdict = mcp.call_tool("project_validate", {
                "content": json.dumps(doc), "format": "json"})
        finally:
            mcp.close()
        if not verdict.get("valid"):
            return _fail(state, f"project model invalid: {verdict.get('violations')}")
        return {"project_doc": doc, "stage": "generate"}

    def generate(state: RunState) -> dict:
        if state.get("generated"):
            return {"stage": "content"}
        b = state["brief"]
        mcp = deps.mcp_factory()
        try:
            outcome = mcp.call_tool("product_build", {
                "outDir": deps.site_rel,
                "vars": {"ProjectName": str(b.get("project_name", "")),
                         "Description": str(b.get("purpose", ""))},
            })
        finally:
            mcp.close()
        if not outcome.get("ok"):
            return _fail(state, f"generation failed: {outcome.get('error')}")
        return {"generated": True, "stage": "content"}

    def content(state: RunState) -> dict:
        attempts = dict(state.get("attempts", {}))
        if state.get("failed"):
            return {"stage": "verify"}
        if state.get("content_done"):
            # Already applied: a repair cycle must not rewrite it, and
            # the placeholder contract no longer holds afterwards.
            return {"stage": "verify"}
        used = int(attempts.get("content", 0))
        if used >= deps.max_content_attempts:
            return _fail(state, "content budget spent")
        path = os.path.join(deps.allowed_root, deps.site_rel,
                            "src", "pages", "index.astro")
        try:
            with open(path) as f:
                page = f.read()
        except OSError as e:
            return _fail(state, f"reading generated index: {e}")
        if page.count(FAQ_PLACEHOLDER) != EXPECTED_FAQ_COUNT:
            return _fail(state,
                         "generated index lacks the FAQ contract "
                         f"(found {page.count(FAQ_PLACEHOLDER)}, want "
                         f"{EXPECTED_FAQ_COUNT} labeled answers; the "
                         "content pass targets the SEO/AEO template "
                         "upgrade)")
        b = state["brief"]
        prompt = (
            "Write exactly 3 FAQ answers as JSON {\"answers\": [...]}. "
            f"Tone: {b.get('tone_of_voice')}. Audience: {b.get('target_audience')} "
            f"({b.get('ideal_customer_profile')}). Keywords: {b.get('primary_keywords')}. "
            f"Geography: {b.get('geographic_scope')}. "
            "Each answer: one or two plain sentences, under 500 characters, "
            "no placeholders, no invented statistics, no superlatives about "
            "being world-class."
        )
        try:
            from stitch import design_summary as _direction
            direction = _direction(state.get("design", {}))
        except Exception:
            direction = ""
        if direction:
            prompt += f" Design direction to honor: {direction}."
        raw = deps.llm("documentation", [
            {"role": "system", "content": "You write honest marketing copy as JSON."},
            {"role": "user", "content": prompt},
        ])
        answers = _parse_answers(raw)
        err = _check_answers(page, answers)
        attempts["content"] = used + 1
        if err:
            if used + 1 >= deps.max_content_attempts:
                return dict(_fail(state, f"content rejected: {err}"),
                            attempts=attempts)
            return {"attempts": attempts, "stage": "content"}
        for ans in answers:
            quoted = _first_placeholder(page)
            if not quoted:
                return dict(_fail(state, "placeholder contract broken"),
                            attempts=attempts)
            page = page.replace(quoted, '"' + ans + '"', 1)
        try:
            with open(path, "w") as f:
                f.write(page)
        except OSError as e:
            return _fail(state, f"writing generated index: {e}")
        return {"attempts": attempts, "content_done": True,
                "stage": "verify"}

    def verify(state: RunState) -> dict:
        if state.get("failed"):
            return {"stage": "report"}
        root = os.path.join(deps.allowed_root, deps.site_rel)
        index = os.path.join(root, "src", "pages", "index.astro")
        manifest = os.path.join(root, "repoforge.manifest.json")
        problems = []
        for rel in ("package.json", "src/pages/index.astro", "src/site.ts",
                    "repoforge.manifest.json"):
            if not os.path.isfile(os.path.join(root, rel)):
                problems.append(f"missing {rel}")
        try:
            with open(index) as f:
                page = f.read()
            if "{{" in page:
                problems.append("unresolved placeholders")
            if page.count("<h1") != 1:
                problems.append("h1 count != 1")
            if FAQ_PLACEHOLDER in page:
                problems.append("FAQ answers not written")
        except OSError as e:
            problems.append(f"unreadable index: {e}")
        try:
            with open(manifest) as f:
                man = json.load(f)
            if not man.get("repoforge_version"):
                problems.append("manifest lacks engine version")
        except (OSError, ValueError) as e:
            problems.append(f"manifest unreadable: {e}")
        if problems:
            repairs = int(state.get("repair_rounds", 0))
            if repairs >= deps.max_repair_rounds:
                return _fail(state, f"verification failed: {problems}")
            return {"repair_rounds": repairs + 1, "stage": "content"}
        return {"verified": True, "stage": "report"}

    def report(state: RunState) -> dict:
        failure = ""
        if state.get("failed"):
            failure = (state.get("errors") or ["unknown failure"])[-1]
        out = dict(_report(state, failure))
        out.update({"done": True, "stage": "done"})
        return out

    graph = StateGraph(RunState)
    graph.add_node("intake", intake)
    graph.add_node("spec_draft", spec_draft)
    graph.add_node("approve_gate", approve_gate)
    graph.add_node("design", design)
    graph.add_node("project_model", project_model)
    graph.add_node("generate", generate)
    graph.add_node("content", content)
    graph.add_node("verify", verify)
    graph.add_node("report", report)
    graph.add_edge(START, "intake")
    graph.add_edge("intake", "spec_draft")
    graph.add_edge("spec_draft", "approve_gate")
    graph.add_edge("approve_gate", "design")
    graph.add_edge("design", "project_model")
    graph.add_edge("project_model", "generate")
    graph.add_edge("generate", "content")
    graph.add_edge("content", "verify")
    graph.add_conditional_edges("verify", _after_verify,
                                {"report": "report", "content": "content"})
    graph.add_edge("report", END)
    return graph.compile(checkpointer=checkpointer)


def _after_verify(state: RunState) -> str:
    if state.get("failed"):
        return "report"
    if state.get("verified"):
        return "report"
    return "content"


def _parse_answers(raw: str):
    try:
        data = json.loads(raw)
    except ValueError:
        return None
    if not isinstance(data, dict):
        return None
    answers = data.get("answers")
    if not isinstance(answers, list):
        return None
    return answers


def _check_answers(page: str, answers) -> str:
    if not isinstance(answers, list) or len(answers) != EXPECTED_FAQ_COUNT:
        return "need exactly 3 answers"
    for a in answers:
        if not isinstance(a, str) or not a.strip():
            return "answers must be non-blank strings"
        if len(a) > MAX_ANSWER_CHARS:
            return "answer exceeds length bound"
        if "{{" in a or "}}" in a:
            return "answer must not contain placeholder syntax"
        if FAQ_PLACEHOLDER in a:
            return "answer must replace the placeholder"
    return ""


def _first_placeholder(page: str) -> str:
    """The full quoted placeholder string for the first FAQ answer,
    so replacement swaps whole strings, never prefixes."""
    idx = page.find(FAQ_PLACEHOLDER)
    if idx < 0:
        return ""
    start = page.rfind('"', 0, idx)
    end = page.find('"', idx)
    if start < 0 or end < 0:
        return ""
    return page[start:end + 1]
