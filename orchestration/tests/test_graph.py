"""Graph tests: fully offline with stubbed LLM and fake MCP.

The fakes prove workflow logic (transitions, bounds, resume); the
engine itself is covered by Go tests, and one marked integration
test drives the real `repoforge mcp serve` when REPOFFORGE_BIN is set.
"""

import json
import os
import sqlite3
import sys

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.types import Command

from brief import REQUIRED, missing_answers
from graph import Deps, build_graph, digest

FULL_BRIEF = {
    "project_name": "demo-site",
    "purpose": "Demonstration site.",
    "ideal_customer_profile": "Operators.",
    "target_audience": "Reviewers.",
    "tone_of_voice": "Plain and direct.",
    "primary_keywords": "demo, site",
    "geographic_scope": "Worldwide.",
    "non_goals": "SaaS operation.",
    "success_metric": "Completed demo builds.",
}

SITE_FILES = {
    "package.json": '{"name": "demo-site"}',
    "src/pages/index.astro": (
        "<h1>x</h1>\n"
        '"Replace this answer one."\n'
        '"Replace this answer two."\n'
        '"Replace this answer three."\n'
    ),
    "src/site.ts": "export const x = 1;\n",
    "repoforge.manifest.json": '{"repoforge_version": "test"}',
}


class StubLLM:
    """Scripted responses per task; records every call."""

    def __init__(self, scripts=None):
        self.scripts = dict(scripts or {})
        self.calls = []

    def __call__(self, task, messages):
        self.calls.append(task)
        queue = self.scripts.get(task, [])
        if queue:
            return queue.pop(0)
        return self.scripts.get("__default__", "ok")


class FakeMCP:
    """In-memory engine stand-in operating on a temp allowed root."""

    def __init__(self, root, break_tree=False):
        self.root = root
        self.calls = []
        self.break_tree = break_tree

    def call_tool(self, name, arguments):
        self.calls.append(name)
        if name == "project_validate":
            doc = json.loads(arguments["content"])
            if doc.get("name") == "INVALID":
                return {"valid": False,
                        "violations": [{"field": "name", "message": "bad"}]}
            return {"valid": True, "violations": []}
        if name == "product_build":
            out = os.path.join(self.root, arguments["outDir"])
            os.makedirs(out, exist_ok=True)
            for rel, body in SITE_FILES.items():
                if self.break_tree and rel == "package.json":
                    continue
                p = os.path.join(out, rel)
                os.makedirs(os.path.dirname(p), exist_ok=True)
                with open(p, "w") as f:
                    f.write(body)
            return {"ok": True, "outDir": arguments["outDir"]}
        raise AssertionError(f"unexpected tool {name}")

    def close(self):
        pass


def resume(graph, config, value):
    """Consume the resume stream to completion. An un-iterated
    `stream()` generator executes nothing, which is a test bug, not
    graph behavior."""
    for _ in graph.stream(Command(resume=value), config,
                          stream_mode="updates"):
        pass
    return graph.get_state(config).values


def make_deps(tmp_path, llm, mcp=None):
    mcp = mcp if mcp is not None else FakeMCP(str(tmp_path))
    return Deps(llm=llm, mcp_factory=lambda: mcp,
                allowed_root=str(tmp_path), site_rel="site"), mcp


def run_until_pause(graph, config, initial=None):
    outcome = {"paused": None, "done": None}
    stream = (graph.stream(initial, config, stream_mode="updates")
              if initial is not None
              else graph.stream(None, config, stream_mode="updates"))
    for _ in stream:
        pass
    snapshot = graph.get_state(config)
    values = snapshot.values or {}
    if values.get("done"):
        outcome["done"] = values
        return outcome
    tasks = snapshot.tasks
    assert tasks and tasks[0].interrupts, "run must pause or finish"
    outcome["paused"] = tasks[0].interrupts[0].value
    return outcome


def compiled(tmp_path, deps):
    db = os.path.join(str(tmp_path), "runs.sqlite3")
    conn = sqlite3.connect(db, check_same_thread=False)
    return build_graph(deps, checkpointer=SqliteSaver(conn))


def test_happy_path(tmp_path):
    llm = StubLLM({
        "spec-drafting": ["# Spec\n\nDemo."],
        "documentation": [json.dumps({"answers": ["A one.", "A two.", "A three."]})],
    })
    deps, mcp = make_deps(tmp_path, llm)
    graph = compiled(tmp_path, deps)
    config = {"configurable": {"thread_id": "t1"}}
    initial = {"run_id": "t1", "brief": dict(FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = run_until_pause(graph, config, initial)
    assert outcome["paused"]["action"] == "approve-spec"
    digest_value = outcome["paused"]["artifact"]
    resumed = graph.stream(
        Command(resume={"approved": True, "artifact": digest_value,
                        "by": "tester"}),
        config, stream_mode="updates")
    for _ in resumed:
        pass
    values = graph.get_state(config).values
    assert values["stage"] == "done", values.get("report")
    assert values["generated"] and values["verified"]
    assert mcp.calls.count("product_build") == 1
    index = os.path.join(str(tmp_path), "site", "src", "pages", "index.astro")
    with open(index) as f:
        assert "A one." in f.read()


def test_intake_pauses_for_missing_answers(tmp_path):
    llm = StubLLM({"spec-drafting": ["# Spec"], "documentation": [
        json.dumps({"answers": ["A.", "B.", "C."]})]})
    deps, _ = make_deps(tmp_path, llm)
    graph = compiled(tmp_path, deps)
    config = {"configurable": {"thread_id": "t2"}}
    brief = {"project_name": "demo-site"}
    outcome = run_until_pause(
        graph, config,
        {"run_id": "t2", "brief": brief, "stage": "intake",
         "attempts": {}, "errors": []})
    keys = [q["key"] for q in outcome["paused"]["questions"]]
    assert "purpose" in keys and "project_name" not in keys
    answers = {k: "filled " + k for k in keys}
    values = resume(graph, config, answers)
    # The run now waits at the approval gate, brief complete.
    snapshot = graph.get_state(config)
    assert snapshot.values["brief"]["purpose"] == "filled purpose"


def test_approval_mismatch_repauses_within_bounds(tmp_path):
    llm = StubLLM({"spec-drafting": ["# Spec"], "documentation": [
        json.dumps({"answers": ["A.", "B.", "C."]})]})
    deps, _ = make_deps(tmp_path, llm)
    deps = Deps(llm=deps.llm, mcp_factory=deps.mcp_factory,
                allowed_root=deps.allowed_root, site_rel=deps.site_rel,
                max_approval_rounds=2)
    graph = compiled(tmp_path, deps)
    config = {"configurable": {"thread_id": "t3"}}
    initial = {"run_id": "t3", "brief": dict(FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = run_until_pause(graph, config, initial)
    good = outcome["paused"]["artifact"]
    # Wrong artifact twice: re-pause, then exhaustion fails the run.
    for _ in range(2):
        values = resume(graph, config, {"approved": True,
                                        "artifact": "stale",
                                        "by": "tester"})
        if values.get("failed"):
            break
    values = graph.get_state(config).values
    assert values.get("failed"), "stale approvals must exhaust, not pass"
    assert good and len(llm.calls) >= 1


def test_content_retry_then_success(tmp_path):
    llm = StubLLM({
        "spec-drafting": ["# Spec"],
        "documentation": [
            json.dumps({"answers": ["ok", "only-two"]}),
            json.dumps({"answers": ["A one.", "A two.", "A three."]}),
        ],
    })
    deps, _ = make_deps(tmp_path, llm)
    graph = compiled(tmp_path, deps)
    config = {"configurable": {"thread_id": "t4"}}
    initial = {"run_id": "t4", "brief": dict(FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = run_until_pause(graph, config, initial)
    values = resume(graph, config,
                    {"approved": True,
                     "artifact": outcome["paused"]["artifact"],
                     "by": "tester"})
    for _ in graph.stream(None, config, stream_mode="updates"):
        pass
    assert values["stage"] == "done", values.get("report")
    assert llm.calls.count("documentation") == 2


def test_repair_exhaustion_fails_with_report(tmp_path):
    llm = StubLLM({
        "spec-drafting": ["# Spec"],
        "documentation": [json.dumps({"answers": ["A.", "B.", "C."]})],
    })
    mcp = FakeMCP(str(tmp_path), break_tree=True)
    deps, _ = make_deps(tmp_path, llm, mcp)
    graph = compiled(tmp_path, deps)
    config = {"configurable": {"thread_id": "t5"}}
    initial = {"run_id": "t5", "brief": dict(FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = run_until_pause(graph, config, initial)
    values = resume(graph, config,
                    {"approved": True,
                     "artifact": outcome["paused"]["artifact"],
                     "by": "tester"})
    for _ in graph.stream(None, config, stream_mode="updates"):
        pass
    assert values.get("failed"), "broken tree must fail, not loop"
    assert "verification failed" in values.get("report", "")


def test_checkpoint_resume_generates_once(tmp_path):
    llm = StubLLM({
        "spec-drafting": ["# Spec"],
        "documentation": [json.dumps({"answers": ["A.", "B.", "C."]})],
    })
    deps, mcp = make_deps(tmp_path, llm)
    db = os.path.join(str(tmp_path), "runs.sqlite3")
    conn = sqlite3.connect(db, check_same_thread=False)
    graph = build_graph(deps, checkpointer=SqliteSaver(conn))
    config = {"configurable": {"thread_id": "t6"}}
    initial = {"run_id": "t6", "brief": dict(FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = run_until_pause(graph, config, initial)
    assert outcome["paused"]["action"] == "approve-spec"
    assert mcp.calls.count("product_build") == 0
    # Fresh instance, same thread: resume approval, then finish.
    # Generation must happen exactly once, on the new instance only.
    deps2, mcp2 = make_deps(tmp_path, llm)
    conn2 = sqlite3.connect(db, check_same_thread=False)
    graph2 = build_graph(deps2, checkpointer=SqliteSaver(conn2))
    resume(graph2, config,
           {"approved": True,
            "artifact": outcome["paused"]["artifact"],
            "by": "tester"})
    for _ in graph2.stream(None, config, stream_mode="updates"):
        pass
    values = graph2.get_state(config).values
    assert values["stage"] == "done"
    assert mcp.calls.count("product_build") == 0
    assert mcp2.calls.count("product_build") == 1


def test_brief_helpers():
    assert set(REQUIRED) >= {"project_name", "purpose",
                             "ideal_customer_profile"}
    assert "purpose" in missing_answers({"project_name": "x"})
    assert missing_answers(dict(FULL_BRIEF)) == []


def test_digest_stable():
    assert digest("x") == digest("x")
    assert digest("x") != digest("y")


@pytest.mark.integration
def test_real_mcp_product_build(tmp_path):
    """Drives the real `repoforge mcp serve` when REPOFFORGE_BIN is
    set; skipped otherwise (engine covered by Go tests)."""
    binary = os.environ.get("REPOFFORGE_BIN", "")
    if not binary:
        pytest.skip("REPOFFORGE_BIN not set")
    import sys as _sys
    _sys.path.insert(0, os.path.dirname(os.path.dirname(
        os.path.abspath(__file__))))
    from mcp_client import Client
    client = Client(binary, str(tmp_path))
    try:
        verdict = client.call_tool("project_validate", {
            "content": '{"schemaVersion": "v1", "name": "demo", '
                       '"purpose": "Demo."}',
            "format": "json"})
        assert verdict.get("valid") is True
    finally:
        client.close()


def test_content_fails_honestly_without_faq_contract(tmp_path):
    """A generated index without the FAQ contract fails with a precise,
    actionable message instead of writing nothing silently."""
    class NoFaqMCP(FakeMCP):
        def call_tool(self, name, arguments):
            outcome = super().call_tool(name, arguments)
            if name == "product_build":
                out = os.path.join(self.root, arguments["outDir"])
                with open(os.path.join(out, "src", "pages", "index.astro"),
                          "w") as f:
                    f.write("<h1>x</h1>\n")
            return outcome

    llm = StubLLM({"spec-drafting": ["# Spec"],
                   "documentation": [json.dumps({"answers": ["A.", "B.", "C."]})]})
    mcp = NoFaqMCP(str(tmp_path))
    deps, _ = make_deps(tmp_path, llm, mcp)
    graph = compiled(tmp_path, deps)
    config = {"configurable": {"thread_id": "t7"}}
    initial = {"run_id": "t7", "brief": dict(FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = run_until_pause(graph, config, initial)
    values = resume(graph, config,
                    {"approved": True,
                     "artifact": outcome["paused"]["artifact"],
                     "by": "tester"})
    assert values.get("failed")
    report = values.get("report", "")
    assert "FAQ contract" in report, report
    assert "resumable" in report
    # The placeholder answers were never rewritten: nothing silent.
    with open(os.path.join(str(tmp_path), "site", "src", "pages",
                           "index.astro")) as f:
        assert f.read().strip() == "<h1>x</h1>"


def test_verify_never_passes_a_failed_run(tmp_path):
    """A failed content pass must not be contradicted by a later verify."""
    class NoFaqMCP(FakeMCP):
        def call_tool(self, name, arguments):
            outcome = super().call_tool(name, arguments)
            if name == "product_build":
                out = os.path.join(self.root, arguments["outDir"])
                with open(os.path.join(out, "src", "pages", "index.astro"),
                          "w") as f:
                    f.write("<h1>x</h1>\n")
                    for _ in range(3):
                        f.write('"Replace this answer."\n')
            return outcome

    llm = StubLLM({"spec-drafting": ["# Spec"],
                   "documentation": ["not json"] * 5})
    mcp = NoFaqMCP(str(tmp_path))
    deps, _ = make_deps(tmp_path, llm, mcp)
    graph = compiled(tmp_path, deps)
    config = {"configurable": {"thread_id": "t8"}}
    initial = {"run_id": "t8", "brief": dict(FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = run_until_pause(graph, config, initial)
    values = resume(graph, config,
                    {"approved": True,
                     "artifact": outcome["paused"]["artifact"],
                     "by": "tester"})
    assert values.get("failed")
    assert values.get("verified") is None
    assert "content rejected" in values.get("report", "")
