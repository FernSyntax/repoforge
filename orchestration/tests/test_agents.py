"""Review-mesh tests: fully offline, no network, no credentials.

Covers agents.py auditors plus the graph's review node behavior:
advisory warnings never fail a good run, security findings block
verify, and the spec critic is recorded without blocking approval.
"""

import json
import os
import re
import sqlite3
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import agents
from agents import (
    audit_accessibility,
    audit_i18n,
    audit_performance,
    audit_security,
    audit_seo,
    run_review_suite,
    spec_critique,
)


def write_tree(root, files):
    for rel, body in files.items():
        path = os.path.join(root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(body)


HOST_RE = re.compile("https?://[^\\s\"'>)]+")


def hosts_in(text: str) -> set:
    """Hostnames present in http(s) URLs.

    Assertions on hostnames are precise. A substring assertion would
    also pass for `fonts.googleapis.com.evil.com`, which is the exact
    defect CodeQL flags as incomplete URL substring sanitization.
    """
    import agents
    return {agents.url_host(m.group(0)) for m in HOST_RE.finditer(text)}


GOOD_TREE = {
    "package.json": '{"name": "demo"}',
    "src/pages/index.astro": (
        "---\nimport Base from '../layouts/Base.astro';\n---\n"
        "<Base title=\"Demo\" description=\"Demo site\">\n"
        "<h1>Demo</h1>\n<p>Demo site for operators.</p>\n</Base>\n"
    ),
    "src/layouts/Base.astro": (
        "<html lang=\"en\">\n<head><title>Demo</title>\n"
        '<meta name="description" content="Demo site" />\n</head>\n'
        '<body><a href="#main">skip to content</a>\n'
        "<header></header><main id=\"main\"><slot /></main>\n"
        "<style>:focus-visible { outline: 2px solid; }\n"
        "@media (prefers-reduced-motion: reduce) { * { animation: none; } }\n"
        "</style></body></html>\n"
    ),
    "src/site.ts": "export const x = 1;\n",
    "repoforge.manifest.json": '{"repoforge_version": "test"}',
}


def test_good_tree_is_clean(tmp_path):
    write_tree(str(tmp_path), GOOD_TREE)
    suite = run_review_suite(str(tmp_path), {"primary_keywords": "demo"})
    assert suite["security"]["ok"], suite["security"]["issues"]
    assert suite["accessibility"]["ok"], suite["accessibility"]["issues"]
    assert suite["performance"]["ok"], suite["performance"]["issues"]


def test_a11y_flags_missing_alt_and_h1(tmp_path):
    write_tree(str(tmp_path), dict(GOOD_TREE, **{
        "src/pages/index.astro": "<h1>A</h1><h1>B</h1><img src=\"x.png\">\n",
    }))
    rep = audit_accessibility(str(tmp_path))
    assert not rep["ok"]
    assert any("h1" in i for i in rep["issues"])
    assert any("alt" in i for i in rep["issues"])


def test_seo_warns_without_blocking(tmp_path):
    write_tree(str(tmp_path), dict(GOOD_TREE, **{
        "src/pages/index.astro": "<h1>Hi</h1><p>nothing optimized</p>\n",
        "src/layouts/Base.astro": "<html><body><slot /></body></html>",
    }))
    rep = audit_seo(str(tmp_path))
    assert not rep["ok"]
    assert any("title" in i.lower() for i in rep["issues"])


def test_security_blocks_secret_material(tmp_path):
    write_tree(str(tmp_path), dict(GOOD_TREE, **{
        "src/site.ts": "export const k = \"FREELLMAPI_KEY=live\";\n",
    }))
    rep = audit_security(str(tmp_path))
    assert not rep["ok"]
    assert any("secret" in i.lower() for i in rep["issues"])


def test_security_blocks_stitch_key_shape(tmp_path):
    write_tree(str(tmp_path), dict(GOOD_TREE, **{
        "src/site.ts": "export const k = \"AQ.abcdefgh12345678\";\n",
    }))
    rep = audit_security(str(tmp_path))
    assert not rep["ok"]


def test_perf_flags_trackers(tmp_path):
    write_tree(str(tmp_path), dict(GOOD_TREE, **{
        "src/pages/index.astro": (
            "<h1>Hi</h1>\n"
            '<script src="https://www.google-analytics.com/analytics.js">'
            "</script>\n"
        ),
    }))
    rep = audit_performance(str(tmp_path))
    assert not rep["ok"]
    assert any("tracker" in i.lower() for i in rep["issues"])


def test_i18n_notes_regional_scope(tmp_path):
    write_tree(str(tmp_path), GOOD_TREE)
    rep = audit_i18n(str(tmp_path), {"geographic_scope": "Germany"})
    assert any("hreflang" in i.lower() for i in rep["issues"])


def test_spec_critique_is_bounded_and_safe():
    calls = []

    def fake_llm(task, messages):
        calls.append(task)
        return "1. Scope is clear.\n2. Add measurable criteria."

    out = spec_critique(fake_llm, "# Spec\nDemo.", {"project_name": "d"})
    assert "Scope" in out
    assert calls == ["architecture-review"]
    assert spec_critique(fake_llm, "   ", {}) == ""

    def boom(task, messages):
        raise RuntimeError("gateway down")

    assert spec_critique(boom, "# Spec", {}) == ""


def test_graph_records_review_and_still_passes(tmp_path):
    from langgraph.checkpoint.sqlite import SqliteSaver
    from langgraph.types import Command
    from graph import Deps, build_graph
    import test_graph as tg

    llm = tg.StubLLM({
        "spec-drafting": ["# Spec\n\nDemo."],
        "architecture-review": ["No blocking issues."],
        "documentation": [json.dumps(
            {"answers": ["A one.", "A two.", "A three."]})],
    })
    mcp = tg.FakeMCP(str(tmp_path))
    deps = Deps(llm=llm, mcp_factory=lambda: mcp,
                # Design application is mandatory: a journey test must
                # supply a Stitch client, and the static build stays
                # offline (NFR-007) via the CDN fallback.
                stitch_factory=lambda: tg.DesignStitch(),
                run_cmd=lambda cmd, cwd, timeout: type(
                    "R", (), {"returncode": 1})(),
                allowed_root=str(tmp_path), site_rel="site")
    db = os.path.join(str(tmp_path), "runs.sqlite3")
    conn = sqlite3.connect(db, check_same_thread=False)
    graph = build_graph(deps, checkpointer=SqliteSaver(conn))
    config = {"configurable": {"thread_id": "mesh1"}}
    initial = {"run_id": "mesh1", "brief": dict(tg.FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = tg.run_until_pause(graph, config, initial)
    values = tg.resume(graph, config,
                       {"approved": True,
                        "artifact": outcome["paused"]["artifact"],
                        "by": "tester"})
    for _ in graph.stream(None, config, stream_mode="updates"):
        pass
    values = graph.get_state(config).values
    assert values["stage"] == "done", values.get("report")
    assert values["verified"]
    assert "agents:" in values.get("report", "")
    assert values.get("spec_critique") == "No blocking issues."
    assert set(values.get("agent_reports", {})) >= {
        "accessibility", "seo", "security", "performance", "i18n"}


def test_graph_blocks_on_leaked_secret(tmp_path):
    from langgraph.checkpoint.sqlite import SqliteSaver
    from graph import Deps, build_graph
    import test_graph as tg

    class LeakyMCP(tg.FakeMCP):
        def call_tool(self, name, arguments):
            outcome = super().call_tool(name, arguments)
            if name == "product_build":
                out = os.path.join(self.root, arguments["outDir"])
                with open(os.path.join(out, "src", "site.ts"), "w") as f:
                    f.write("export const k = \"AQ.abcdefgh12345678\";\n")
            return outcome

    llm = tg.StubLLM({
        "spec-drafting": ["# Spec"],
        "architecture-review": ["No blocking issues."],
        "documentation": [json.dumps({"answers": ["A.", "B.", "C."]})],
    })
    deps = Deps(llm=llm, mcp_factory=lambda: LeakyMCP(str(tmp_path)),
                stitch_factory=lambda: tg.DesignStitch(),
                run_cmd=lambda cmd, cwd, timeout: type(
                    "R", (), {"returncode": 1})(),
                allowed_root=str(tmp_path), site_rel="site")
    db = os.path.join(str(tmp_path), "runs.sqlite3")
    conn = sqlite3.connect(db, check_same_thread=False)
    graph = build_graph(deps, checkpointer=SqliteSaver(conn))
    config = {"configurable": {"thread_id": "mesh2"}}
    initial = {"run_id": "mesh2", "brief": dict(tg.FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = tg.run_until_pause(graph, config, initial)
    tg.resume(graph, config,
              {"approved": True,
               "artifact": outcome["paused"]["artifact"], "by": "tester"})
    for _ in graph.stream(None, config, stream_mode="updates"):
        pass
    values = graph.get_state(config).values
    assert values.get("failed")
    assert "secret" in values.get("report", "").lower()


def test_unwrap_mcp_content_envelope():
    """Stitch returns MCP content envelopes; the graph must read names
    from structuredContent or embedded JSON text, not just top level."""
    import graph as graph_mod

    assert graph_mod._resource_name(
        {"structuredContent": {"name": "projects/123"}}, "project") == \
        "projects/123"
    assert graph_mod._resource_name(
        {"content": [{"text": '{"name": "screens/abc"}', "type": "text"}]},
        "screen") == "screens/abc"
    assert graph_mod._resource_name({"name": "projects/flat"},
                                    "project") == "projects/flat"
    assert graph_mod._resource_name({"unrelated": 1}, "project") == ""
    assert graph_mod._resource_name("not-a-dict", "project") == ""


FIXTURE_HTML = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                            "fixtures", "stitch-screen.html")


def fixture_html():
    with open(FIXTURE_HTML) as f:
        return f.read()


def astro_tree(root):
    write_tree(root, dict(GOOD_TREE, **{
        "src/layouts/Base.astro": (
            "<html lang=\"en\">\n<head><title>Demo</title></head>\n"
            "<body><slot /></body></html>\n"
        ),
    }))
    os.makedirs(os.path.join(root, "src", "styles"), exist_ok=True)
    os.makedirs(os.path.join(root, "src", "components"), exist_ok=True)
    with open(os.path.join(root, "src", "styles", "global.css"), "w") as f:
        f.write(":root {\n  --bg: #ffffff;\n  --ink: #111111;\n}\n"
                "body { color: var(--ink); }\n")
    with open(os.path.join(root, "src", "components", "Header.astro"),
              "w") as f:
        f.write("---\nimport { SITE_NAME } from \"../site\";\n---\n"
                "\n<header class=\"site-header\">\n"
                "  <nav aria-label=\"Primary\">\n"
                "    <a class=\"brand\" href=\"/\">{SITE_NAME}</a>\n"
                "  </nav>\n</header>\n")
    with open(os.path.join(root, "src", "components", "Footer.astro"),
              "w") as f:
        f.write("---\nimport { SITE_NAME } from \"../site\";\n---\n"
                "\n<footer class=\"site-footer\">\n"
                "  <nav aria-label=\"Footer\">\n"
                "    <span>{SITE_NAME}</span>\n"
                "  </nav>\n</footer>\n")
    with open(os.path.join(root, "src", "pages", "index.astro"), "w") as f:
        f.write("<h1>Demo</h1>\n"
                "<section class=\"hero\"><p>Brief copy.</p></section>\n"
                '"Replace this answer one."\n'
                '"Replace this answer two."\n'
                '"Replace this answer three."\n')


def test_parse_design_html_finds_nav_and_footer():
    import agents as agents_mod
    parsed = agents_mod.parse_design_html(fixture_html())
    nav_labels = [label for label, _ in parsed["nav"]]
    footer_labels = [label for label, _ in parsed["footer"]]
    assert "Platform Overview" in nav_labels
    assert "Golden Paths" in nav_labels
    assert any("Support" in label for label in footer_labels)


def test_map_anchor_rewires_to_template_anchors():
    import agents as agents_mod
    assert agents_mod.map_anchor("Golden Paths") == "#features"
    assert agents_mod.map_anchor("Docs") == "#faq"
    assert agents_mod.map_anchor("Request Demo") == "#contact"
    assert agents_mod.map_anchor("Governance & Compliance") is None


def test_claim_labels_dropped():
    import agents as agents_mod
    assert agents_mod._clean_label("SOC2 Type II Report") == ""
    assert agents_mod._clean_label("99.99% uptime") == ""
    assert agents_mod._clean_label("Platform Overview") == \
        "Platform Overview"


def test_stitch_tokens_from_dark_theme():
    import agents as agents_mod
    tokens = agents_mod.stitch_tokens({
        "namedColors": {"background": "#101419",
                        "on_background": "#e0e2ea",
                        "surface_container": "#1c2025",
                        "primary_container": "#00e5ff"},
        "bodyFontFamily": "Inter",
    })
    assert tokens["--bg"] == "#101419"
    assert tokens["--ink"] == "#e0e2ea"
    assert tokens["--accent"] == "#00e5ff"
    assert "Inter" in tokens["--font"]
    assert agents_mod.stitch_tokens({}) == {}
    assert agents_mod.stitch_tokens(None) == {}


def test_apply_copies_design_not_invented_facts(tmp_path):
    import agents as agents_mod
    astro_tree(str(tmp_path))
    theme = {"namedColors": {"background": "#101419",
                             "on_background": "#e0e2ea",
                             "primary_container": "#00e5ff"},
             "bodyFontFamily": "Inter"}
    outcome = agents_mod.apply_stitch_to_tree(
        str(tmp_path), fixture_html(), theme)
    assert any(a.startswith("header nav:") for a in outcome["applied"])
    assert any(a.startswith("tokens:") for a in outcome["applied"])
    assert any("SOC2" in s for s in outcome["skipped"])
    header = open(os.path.join(str(tmp_path), "src", "components",
                               "Header.astro")).read()
    assert "#features" in header and "#faq" in header
    assert "SOC2" not in header and "tailwind" not in header.lower()
    assert "<script" not in header
    css = open(os.path.join(str(tmp_path), "src", "styles",
                            "global.css")).read()
    assert "--bg: #101419" in css
    index = open(os.path.join(str(tmp_path), "src", "pages",
                              "index.astro")).read()
    assert index.count("<h1") == 1
    assert index.count("Replace this answer") == 3
    assert 'id="top"' in index
    assert "cdn.tailwindcss.com" not in index
    assert "get.northwind.dev" not in index


def test_apply_tolerates_missing_files(tmp_path):
    import agents as agents_mod
    outcome = agents_mod.apply_stitch_to_tree(
        str(tmp_path), fixture_html(), None)
    assert outcome["applied"] == []
    assert any("absent" in s for s in outcome["skipped"])


class FakeStitchDownload:
    """Stitch stand-in serving recorded HTML for design_apply tests."""

    def __init__(self, html=""):
        with open(FIXTURE_HTML) as f:
            self.html = html or f.read()
        self.downloads = []

    def download(self, url):
        self.downloads.append(url)
        return self.html.encode()

    def close(self):
        pass


def design_deps(tmp_path, stitch):
    from graph import Deps
    import test_graph as tg
    mcp = tg.FakeMCP(str(tmp_path))
    site = os.path.join(str(tmp_path), "site")
    write_tree(site, dict(GOOD_TREE, **{
        "src/pages/index.astro": (
            "<h1>x</h1>\n<section class=\"hero\"><p>Hi.</p></section>\n"
            '"Replace this answer one."\n'
            '"Replace this answer two."\n'
            '"Replace this answer three."\n'
        ),
    }))
    for rel, body in {
        "src/styles/global.css": ":root {\n  --bg: #ffffff;\n}\n",
        "src/components/Header.astro": "---\n---\n<header></header>\n",
        "src/components/Footer.astro": "---\n---\n<footer></footer>\n",
    }.items():
        path = os.path.join(site, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(body)
    return Deps(llm=lambda task, messages: "ok",
                mcp_factory=lambda: mcp,
                stitch_factory=lambda: stitch,
                # Static Tailwind build stays offline in tests (NFR-007):
                # force the CDN fallback; dedicated static tests cover
                # the success path with fakes.
                run_cmd=lambda cmd, cwd, timeout: type("R", (),
                                                       {"returncode": 1})(),
                allowed_root=str(tmp_path), site_rel="site")


def design_state():
    return {"run_id": "d1", "brief": dict(FULL_BRIEF), "stage": "generate",
            "attempts": {}, "errors": [],
            "design": {"project": "projects/1",
                       "screen": "projects/1/screens/abc",
                       "html_url": "https://x/page.html",
                       "theme": {"namedColors": {"background": "#101419",
                                                 "on_background": "#e0e2ea"},
                                 "bodyFontFamily": "Inter"}}}


def test_design_apply_downloads_and_applies(tmp_path):
    import test_graph as tg
    from graph import design_apply_stage
    global FULL_BRIEF
    FULL_BRIEF = tg.FULL_BRIEF
    deps = design_deps(tmp_path, FakeStitchDownload())
    out = design_apply_stage(deps, design_state())
    assert out["stage"] == "content"
    applied = out["design"]["applied"]
    assert any(a.startswith("header nav:") for a in applied)
    assert any(a.startswith("tokens:") for a in applied)
    header = open(os.path.join(str(tmp_path), "site", "src", "components",
                               "Header.astro")).read()
    assert "#features" in header
    assert os.path.isfile(os.path.join(str(tmp_path), "design",
                                       "reference.html"))
    assert os.path.isfile(os.path.join(str(tmp_path), "design",
                                       "tokens.json"))


def test_design_apply_idempotent(tmp_path):
    import test_graph as tg
    from graph import design_apply_stage
    global FULL_BRIEF
    FULL_BRIEF = tg.FULL_BRIEF
    stitch = FakeStitchDownload()
    deps = design_deps(tmp_path, stitch)
    first = design_apply_stage(deps, design_state())
    second = design_apply_stage(
        deps, dict(design_state(), design=dict(
            design_state()["design"], applied=first["design"]["applied"])))
    assert second["stage"] == "content"
    assert len(stitch.downloads) == 1


def test_design_apply_is_mandatory_not_optional(tmp_path):
    """Design application must never silently degrade to the template:
    a configured Stitch run that cannot apply fails with an actionable,
    resumable report instead."""
    import test_graph as tg
    from graph import Deps, design_apply_stage
    global FULL_BRIEF
    FULL_BRIEF = tg.FULL_BRIEF
    import graph as graph_mod

    def fresh(tmp):
        site = os.path.join(str(tmp), "site")
        os.makedirs(os.path.join(site, "src", "pages"),
                    exist_ok=True)
        with open(os.path.join(site, "src", "pages", "index.astro"),
                  "w") as f:
            f.write("<h1>x</h1>\n")
        return site

    # 1. No Stitch client configured at all: fails, does not fall back.
    fresh(tmp_path)
    no_client = graph_mod.Deps(
        llm=lambda t, m: "ok",
        mcp_factory=lambda: tg.FakeMCP(str(tmp_path)),
        allowed_root=str(tmp_path), site_rel="site")
    out = graph_mod.design_apply_stage(no_client, design_state())
    assert out.get("failed") is True, out
    assert out.get("done") is True

    # 2. Stitch recorded a skip: fails, does not fall back.
    with_client = graph_mod.Deps(
        llm=lambda t, m: "ok",
        mcp_factory=lambda: tg.FakeMCP(str(tmp_path)),
        stitch_factory=lambda: FakeStitchDownload(),
        run_cmd=lambda c, w, t: type("R", (),
                                      {"returncode": 1})(),
        allowed_root=str(tmp_path), site_rel="site")
    state = design_state()
    state["design"] = {"skipped": "no-stitch-client"}
    out = graph_mod.design_apply_stage(with_client, state)
    assert out.get("failed") is True, out

    # 3. A screen with no HTML reference: fails, does not fall back.
    state2 = design_state()
    del state2["design"]["html_url"]
    out2 = graph_mod.design_apply_stage(with_client, state2)
    assert out2.get("failed") is True, out2

    # 4. The happy path still applies the design (regression guard for
    #    the extraction bug: html_url was sourced only from the
    #    generate envelope, so get_screen responses shipped bare).
    ok = graph_mod.design_apply_stage(with_client, design_state())
    assert ok["stage"] == "content" and ok["design"]["applied"]


def test_token_merge_preserves_unthemed_vars(tmp_path):
    import agents as agents_mod
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "src", "styles"))
    os.makedirs(os.path.join(site, "src", "components"))
    os.makedirs(os.path.join(site, "src", "pages"))
    with open(os.path.join(site, "src", "styles", "global.css"),
              "w") as f:
        f.write(":root {\n  --bg: #ffffff;\n  --radius: 0.5rem;\n}\n")
    outcome = agents_mod.apply_stitch_to_tree(
        site, "<html><body></body></html>",
        {"namedColors": {"background": "#101419"}})
    assert any(a.startswith("tokens:") for a in outcome["applied"])
    css = open(os.path.join(site, "src", "styles", "global.css")).read()
    assert "--bg: #101419" in css
    assert "--radius: 0.5rem" in css


def test_sanitize_keeps_tailwind_and_fonts_drops_risks():
    import agents as agents_mod
    html = fixture_html()
    html += ('<script src="https://evil.example/x.js"></script>'
             '<iframe src="https://evil.example"></iframe>'
             '<a href="javascript:alert(1)" onclick="steal()">Click</a>')
    parts = agents_mod.sanitize_faithful(html)
    assert "cdn.tailwindcss.com" in hosts_in(parts["head"])
    assert "tailwind-config" in parts["head"]
    assert "fonts.googleapis.com" in hosts_in(parts["head"])
    assert "evil.example" not in hosts_in(parts["head"])
    assert "evil.example" not in hosts_in(parts["body"])
    assert "onclick" not in parts["body"]
    assert "javascript:" not in parts["body"]
    assert "<header" in parts["body"] and "<main" in parts["body"]
    assert "non-tailwind script" in parts["dropped"]
    assert "iframe" in parts["dropped"]


def test_demote_extra_h1_keeps_first():
    import agents as agents_mod
    page = "<h1>One</h1><p>x</p><h1 class=\"a\">Two</h1><h1>Three</h1>"
    out = agents_mod._demote_extra_h1(page)
    assert out.count("<h1") == 1 and "<h1>One</h1>" in out
    assert out.count("<h2") == 2
    single = "<h1>Only</h1>"
    assert agents_mod._demote_extra_h1(single) == single


def test_faithful_page_replaces_index(tmp_path):
    import agents as agents_mod
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "src", "pages"))
    with open(os.path.join(site, "src", "pages", "index.astro"),
              "w") as f:
        f.write("<h1>Template</h1>\n\"Replace this answer one.\"\n")
    outcome = agents_mod.apply_faithful_page(
        site, fixture_html(), "demo-site")
    assert any(a.startswith("faithful: index") for a in outcome["applied"])
    page = open(os.path.join(site, "src", "pages",
                             "index.astro")).read()
    assert page.startswith("<!DOCTYPE html>")
    assert page.count("<h1") == 1
    assert "cdn.tailwindcss.com" in hosts_in(page)
    assert "Replace this answer" not in page
    assert "{{" not in page
    assert "human-reviewed" in page or "human-review" in page


def test_faithful_run_skips_content_llm(tmp_path):
    import test_graph as tg
    from graph import Deps, build_graph
    from langgraph.checkpoint.sqlite import SqliteSaver
    llm = tg.StubLLM({
        "spec-drafting": ["# Spec\n\nDemo."],
        "architecture-review": ["No blocking issues."],
        "documentation": [json.dumps({"answers": ["A.", "B.", "C."]})],
    })
    site = os.path.join(str(tmp_path), "site")
    write_tree(site, dict(GOOD_TREE, **{
        "src/pages/index.astro": "<h1>x</h1>\n",
    }))
    for rel, body in {
        "src/styles/global.css": ":root {\n  --bg: #ffffff;\n}\n",
        "src/components/Header.astro": "---\n---\n<header></header>\n",
        "src/components/Footer.astro": "---\n---\n<footer></footer>\n",
    }.items():
        path = os.path.join(site, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(body)

    class FaithfulMCP(tg.FakeMCP):
        def call_tool(self, name, arguments):
            outcome = super().call_tool(name, arguments)
            if name == "product_build":
                out = os.path.join(self.root, arguments["outDir"])
                with open(os.path.join(out, "src", "pages",
                                       "index.astro"), "w") as f:
                    f.write("<h1>x</h1>\n")
            return outcome

    with open(FIXTURE_HTML, "rb") as f:
        faithful_bytes = f.read()

    class FaithfulStitch:
        def download(self, url):
            return faithful_bytes

        def call_tool(self, name, arguments):
            if name == "create_project":
                return {"structuredContent": {"name": "projects/9"}}
            if name == "generate_screen_from_text":
                return {"structuredContent": {"outputComponents": [
                    {"design": {"screens": [{
                        "name": "projects/9/screens/s",
                        "screenshot": {"downloadUrl": "https://x/s.png"},
                        "htmlCode": {"downloadUrl": "https://x/p.html"},
                        "theme": {"namedColors": {
                            "background": "#101419"}}}]}}]}}
            if name == "get_screen":
                return {}
            raise AssertionError(name)

        def close(self):
            pass

    deps = Deps(llm=llm, mcp_factory=lambda: FaithfulMCP(str(tmp_path)),
                stitch_factory=lambda: FaithfulStitch(),
                # Static build stays offline in tests: force the CDN
                # fallback so no npm/network runs here (NFR-007).
                run_cmd=lambda cmd, cwd, timeout: type("R", (),
                                                       {"returncode": 1})(),
                allowed_root=str(tmp_path), site_rel="site")
    db = os.path.join(str(tmp_path), "runs.sqlite3")
    conn = sqlite3.connect(db, check_same_thread=False)
    graph = build_graph(deps, checkpointer=SqliteSaver(conn))
    config = {"configurable": {"thread_id": "faithful1"}}
    initial = {"run_id": "faithful1", "brief": dict(tg.FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    outcome = tg.run_until_pause(graph, config, initial)
    values = tg.resume(graph, config,
                       {"approved": True,
                        "artifact": outcome["paused"]["artifact"],
                        "by": "tester"})
    for _ in graph.stream(None, config, stream_mode="updates"):
        pass
    values = graph.get_state(config).values
    assert values["stage"] == "done", values.get("report")
    assert values["verified"]
    assert values["design"].get("faithful") is True
    assert "documentation" not in llm.calls
    page = open(os.path.join(site, "src", "pages",
                             "index.astro")).read()
    assert "cdn.tailwindcss.com" in hosts_in(page)
    assert "faithful: index" in values.get("report", "")


def test_faithful_marks_scripts_styles_inline():
    import agents as agents_mod
    page = agents_mod.build_faithful_page(
        '<script src="https://cdn.tailwindcss.com"></script>'
        '<script id="tailwind-config">x = 1;</script>'
        '<style>.a { color: red; }</style>', "<h1>Hi</h1>", "", "demo")
    assert page.count("is:inline") == 3
    assert "cdn.tailwindcss.com" in hosts_in(page)


def test_faithful_preserves_html_class():
    import agents as agents_mod
    page = agents_mod.build_faithful_page("", "<h1>Hi</h1>", "",
                                          "demo", ' class="dark"')
    assert '<html lang="en" class="dark">' in page
    parts = agents_mod.sanitize_faithful(
        '<html class="dark" lang="en"><head></head>'
        '<body><h1>Hi</h1></body></html>')
    assert parts["html_attrs"] == ' class="dark"'


def test_icon_ligature_labels_dropped():
    import agents as agents_mod
    assert agents_mod._clean_label("markdown_copy Read Docs") == ""
    assert agents_mod._clean_label("Read Docs") == "Read Docs"


def test_icon_font_text_excluded_from_labels():
    import agents as agents_mod
    parsed = agents_mod.parse_design_html(
        '<header><a href="#x"><span class="material-symbols-outlined">'
        "terminal</span>CLI Quickstart</a></header>")
    assert parsed["nav"] == [("CLI Quickstart", "#x")]


def test_extract_tailwind_config():
    import agents as agents_mod
    extraction = agents_mod.extract_tailwind_config(fixture_html())
    assert extraction["present"] is True
    assert "forms" in extraction["plugins"]
    assert "container-queries" in extraction["plugins"]
    assert extraction["config_js"].startswith("module.exports = {")
    assert 'darkMode' in extraction["config_js"]
    assert agents_mod.extract_tailwind_config("<html></html>")["present"] is False
    assert agents_mod.tailwind_npm_packages(["forms", "evil"]) == [
        "tailwindcss@3", "@tailwindcss/forms"]
    cfg = agents_mod.tailwind_config_file("module.exports = {a: 1};",
                                          ["forms"])
    assert "content:" in cfg and "@tailwindcss/forms" in cfg


def test_rewrite_head_static_and_fonts():
    import agents as agents_mod
    head = ('<link href="https://fonts.googleapis.com/css2?family=X" '
            'rel="stylesheet"/>'
            '<script src="https://cdn.tailwindcss.com"></script>'
            '<script id="tailwind-config">tailwind.config = {};</script>'
            "<title>T</title>")
    static = agents_mod.rewrite_head_static(head)
    assert "cdn.tailwindcss.com" not in hosts_in(static)
    assert "tailwind-config" not in static
    assert "fonts.googleapis.com" in hosts_in(static)
    assert agents_mod.collect_font_css_urls(head) == [
        "https://fonts.googleapis.com/css2?family=X"]
    local = agents_mod.rewrite_fonts_local(head)
    assert "fonts.googleapis.com" not in local
    assert 'href="/fonts.css"' in local


def test_fetch_font_package_rewrites_urls():
    import agents as agents_mod
    css = ("@font-face{src:url(https://fonts.gstatic.com/a.woff2) "
            "format('woff2');}"
            "@font-face{src:url(https://fonts.gstatic.com/b.woff2);}")
    package = agents_mod.fetch_font_package(
        ["https://fonts.googleapis.com/css2?family=X"],
        lambda url: css.encode() if "css2" in url else b"FAKE-" + url[-6:].encode())
    assert "url(/fonts/font-1.woff2)" in package["css"]
    assert package["files"]["font-1.woff2"].startswith(b"FAKE-")
    assert len(package["files"]) == 2


def test_static_build_success_with_fakes(tmp_path):
    import agents as agents_mod
    import graph as graph_mod
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "src", "pages"))
    with open(os.path.join(site, "src", "pages", "index.astro"),
              "w") as f:
        f.write("<!DOCTYPE html>\n<html><head>"
                '<script src="https://cdn.tailwindcss.com"></script>'
                '<script id="tailwind-config">tailwind.config = {};'
                "</script></head><body><h1>Hi</h1></body></html>")
    html = fixture_html()

    class Result:
        returncode = 0

    seen = []

    def fake_run(cmd, cwd, timeout):
        seen.append(cmd)
        if cmd[:2] == ["npx", "tailwindcss"]:
            out = os.path.join(cwd, "src", "styles", "stitch.css")
            os.makedirs(os.path.dirname(out), exist_ok=True)
            with open(out, "w") as f:
                f.write(".dark{color:#fff}")
        return Result()

    real_fetch = graph_mod._fetch_public
    graph_mod._fetch_public = lambda url, timeout=30: (
        b"@font-face{src:url(https://fonts.gstatic.com/f.woff2);}"
        if "css2" in url else b"WOFF2")
    try:
        from graph import Deps
        deps = Deps(run_cmd=fake_run)
        design: dict = {}
        out = graph_mod.static_build_stage(deps, {}, site, html, design)
    finally:
        graph_mod._fetch_public = real_fetch
    assert any(a.startswith("static: Tailwind") for a in out["applied"])
    assert any("self-hosted" in a for a in out["applied"])
    assert design.get("static_css") is True
    page = open(os.path.join(site, "src", "pages", "index.astro")).read()
    assert "cdn.tailwindcss.com" not in page
    assert "../styles/stitch.css" in page
    assert 'href="/fonts.css"' in page
    assert os.path.isfile(os.path.join(site, "public", "fonts.css"))
    assert any(cmd[0] == "npm" for cmd in seen)


def test_static_build_fallback_keeps_cdn(tmp_path):
    import graph as graph_mod
    from graph import Deps
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "src", "pages"))

    class Result:
        returncode = 1

    deps = Deps(run_cmd=lambda cmd, cwd, timeout: Result())
    design: dict = {}
    out = graph_mod.static_build_stage(deps, {}, site, fixture_html(),
                                       design)
    assert "static_css" not in design
    assert any("CDN page kept" in s for s in out["skipped"])


def test_security_skips_build_dirs(tmp_path):
    import agents as agents_mod
    write_tree(str(tmp_path), dict(GOOD_TREE, **{
        "node_modules/vite/package.json": "GITHUB_TOKEN=zzz\n",
        # Split so the pattern literal never appears verbatim in a
        # tracked file — otherwise repoforge validate flags its own
        # scanner fixtures (spec.md §8.2).
        "dist/index.html": "AK" + "IAIOSFODNN7EXAMPLE\n",
        "src/site.ts": "export const x = 1;\n",
    }))
    rep = agents_mod.audit_security(str(tmp_path))
    assert rep["ok"], rep["issues"]


def test_static_build_cleans_workspace(tmp_path):
    import graph as graph_mod
    from graph import Deps
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "src", "pages"))
    with open(os.path.join(site, "src", "pages", "index.astro"),
              "w") as f:
        f.write("<!DOCTYPE html>\n<html><head>"
                '<script src="https://cdn.tailwindcss.com"></script>'
                "</head><body><h1>Hi</h1></body></html>")
    os.makedirs(os.path.join(site, "node_modules", "pkg"))
    open(os.path.join(site, "node_modules", "pkg", "x.js"),
         "w").write("x")

    class Result:
        returncode = 0

    def fake_run(cmd, cwd, timeout):
        if cmd[:2] == ["npx", "tailwindcss"]:
            out = os.path.join(cwd, "src", "styles", "stitch.css")
            os.makedirs(os.path.dirname(out), exist_ok=True)
            open(out, "w").write(".x{}")
        return Result()

    real_fetch = graph_mod._fetch_public
    graph_mod._fetch_public = lambda url, timeout=30: b"css"
    try:
        deps = Deps(run_cmd=fake_run)
        design: dict = {}
        out = graph_mod.static_build_stage(
            deps, {}, site, fixture_html(), design)
    finally:
        graph_mod._fetch_public = real_fetch
    assert not os.path.exists(os.path.join(site, "node_modules"))
    assert not os.path.exists(os.path.join(site, "tailwind.config.js"))
    assert any("workspace cleaned" in a for a in out["applied"])


# --- production layer tests --------------------------------------------------

PROD_TREE = {
    "package.json": '{"name": "demo"}',
    "src/pages/index.astro": (
        "<!DOCTYPE html>\n<html lang=\"en\"><head>\n"
        "<title>Demo platform for engineering teams</title>\n"
        '<meta name="description" content="Demo platform description '
        'that is comfortably long enough for the audit to pass it '
        'without any trouble at all here." />\n'
        '<meta property="og:title" content="Demo" />\n'
        '<meta property="og:type" content="website" />\n'
        '<meta property="og:description" content="Demo" />\n'
        '<meta name="twitter:card" content="summary" />\n'
        '<link rel="canonical" href="https://demo.example/" />\n'
        '<script type="application/ld+json">{"@type":"WebSite"}</script>\n'
        "</head><body><main><h1>Demo</h1><p>Platform engineering for "
        "engineering teams and developers.</p>\n"
        "<h2>What does the platform do?</h2>"
        "<p>It automates delivery.</p>\n"
        "</main></body></html>\n"
    ),
    "public/llms.txt": "# Demo\n- [Home](/) — Demo\n",
    "public/robots.txt": "User-agent: *\nAllow: /\n",
    "public/sitemap.xml": ('<?xml version="1.0"?><urlset><url>'
                           '<loc>https://demo.example/</loc></url>'
                           '</urlset>'),
    "public/og.svg": '<svg xmlns="http://www.w3.org/2000/svg"></svg>',
    "repoforge.manifest.json": '{"repoforge_version": "test"}',
}


def prod_tree(root):
    write_tree(root, dict(PROD_TREE))


def test_seo_head_extras_needs_domain_for_absolute():
    import agents as agents_mod
    without = agents_mod.seo_head_extras({"project_name": "d"}, "")
    with_domain = agents_mod.seo_head_extras(
        {"project_name": "d"}, "example.com")
    tags = " ".join(without["tags"])
    assert "rel=\"canonical\"" not in tags
    assert "og:image" not in tags
    assert any("needs" in s or "no " in s for s in without["skipped"])
    full = " ".join(with_domain["tags"])
    assert 'rel="canonical" href="https://example.com/"' in full
    assert 'og:image" content="https://example.com/og.svg"' in full
    assert agents_mod.absolute_url("x.com", "/") == "https://x.com/"
    assert agents_mod.absolute_url("https://x.com", "/") == ""
    assert agents_mod.absolute_url("", "/") == ""


def test_meta_description_capped():
    import agents as agents_mod
    brief = {"purpose": "A" * 200, "success_metric": "b" * 100}
    out = agents_mod.meta_description(brief)
    assert len(out) <= 155
    assert agents_mod.meta_description({"purpose": "Short one."}) == \
        "Short one."


def test_light_palette_contrast_verified():
    import agents as agents_mod
    palettes = agents_mod.build_palettes({
        "--c-background": "#101419", "--c-on-background": "#e0e2ea",
        "--c-surface": "#1c2025", "--c-on-surface": "#bac9cc",
    })
    for (mode, _s, _t), ratio in palettes["contrast"].items():
        assert mode in ("dark", "light") and ratio >= 4.5, (mode, ratio)
    assert palettes["light"]["--c-background"] != palettes["dark"]["--c-background"]
    assert agents_mod.contrast_ratio("#000000", "#ffffff") > 20


def test_var_indirect_config_preserves_structure():
    import agents as agents_mod
    cfg = ('tailwind.config = {theme: {extend: {colors: '
           '{"primary": "#0b5fff", "surface": "#101419", '
           '"on-surface": "#e0e2ea"}, spacing: {"4": "1rem"}}}};')
    new, palette = agents_mod.var_indirect_config(cfg)
    assert len(palette) == 3
    assert '"rgb(var(--c-primary) / <alpha-value>)"' in new
    assert '"#0b5fff"' not in new
    assert '"1rem"' in new


def test_inject_theme_toggle_into_faithful(tmp_path):
    import agents as agents_mod
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "src", "pages"))
    with open(os.path.join(site, "src", "pages", "index.astro"),
              "w") as f:
        f.write("<!DOCTYPE html><html class=\"dark\"><head>"
                "<title>t</title></head><body><main><h1>x</h1></main>"
                "</body></html>")
    palettes = agents_mod.build_palettes({"--c-background": "#101419",
                                          "--c-on-background": "#e0e2ea"})
    out = agents_mod.inject_theme_toggle(site, palettes)
    assert any("theme" in a for a in out["applied"])
    page = open(os.path.join(site, "src", "pages", "index.astro")).read()
    assert 'id="theme-toggle"' in page and "aria-pressed" in page
    assert "prefers-color-scheme" in page
    assert "--c-background" in page
    assert "theme: already installed" in \
        agents_mod.inject_theme_toggle(site, palettes)["skipped"]


def test_inject_seo_head_with_and_without_domain(tmp_path):
    import agents as agents_mod
    brief = {"project_name": "demo", "purpose": "Demo platform."}
    rel = "src/pages/index.astro"
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "src", "pages"))
    open(os.path.join(site, rel), "w").write(
        "<!DOCTYPE html><html><head></head><body></body></html>")
    agents_mod.inject_seo_head(site, brief, "")
    page = open(os.path.join(site, rel)).read()
    assert 'name="description"' in page
    assert 'property="og:title"' in page
    assert 'application/ld+json' in page
    assert "canonical" not in page
    agents_mod.inject_seo_head(site, brief, "demo.example")
    page = open(os.path.join(site, rel)).read()
    assert 'href="https://demo.example/"' in page


def test_harden_links_marks_external(tmp_path):
    import agents as agents_mod
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "src", "pages"))
    target = os.path.join(site, "src", "pages", "index.astro")
    open(target, "w").write(
        '<a href="/local">a</a><a href="https://x.example">b</a>')
    out = agents_mod.harden_links(site)
    assert out["applied"] and "1 external" in out["applied"][0]
    page = open(target).read()
    assert 'rel="noopener"' in page and 'href="https://x.example"' in page
    assert 'href="/local">' in page


def test_production_auditors(tmp_path):
    import agents as agents_mod
    prod_tree(str(tmp_path))
    brief = {"primary_keywords": "platform engineering, demo",
             "geographic_scope": "Worldwide"}
    domain = "demo.example"
    assert agents_mod.audit_seo_full(str(tmp_path), brief, domain)["ok"]
    assert agents_mod.audit_aeo(str(tmp_path), brief)["ok"]
    assert agents_mod.audit_geo(str(tmp_path), brief)["ok"]
    assert agents_mod.audit_link_safety(str(tmp_path))["ok"]
    assert agents_mod.audit_images(str(tmp_path))["ok"]
    icp = agents_mod.icp_filter_report(
        open(os.path.join(str(tmp_path), "src", "pages",
                          "index.astro")).read(),
        {"purpose": "Demo platform for engineering teams",
         "ideal_customer_profile": "engineering leaders"})
    assert icp["score"] > 0


def test_link_auditor_blocks_javascript_url(tmp_path):
    import agents as agents_mod
    prod_tree(str(tmp_path))
    target = os.path.join(str(tmp_path), "src", "pages", "index.astro")
    page = open(target).read() + '<a href="javascript:x()">bad</a>'
    open(target, "w").write(page)
    rep = agents_mod.audit_link_safety(str(tmp_path))
    assert not rep["ok"]
    assert "javascript" in rep["issues"][0]


def test_write_production_files(tmp_path):
    import agents as agents_mod
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "public"))
    brief = {"project_name": "demo", "purpose": "Demo."}
    out = agents_mod.write_seo_files(site, brief, "demo.example")
    assert any("robots" in a for a in out["applied"])
    assert any("sitemap" in a for a in out["applied"])
    assert any("og.svg" in a for a in out["applied"])
    assert "Sitemap: https://demo.example/sitemap.xml" in \
        open(os.path.join(site, "public", "robots.txt")).read()
    fresh = os.path.join(str(tmp_path), "fresh")
    os.makedirs(os.path.join(fresh, "public"))
    plain = agents_mod.write_seo_files(fresh, brief, "")
    assert any("sitemap.xml needs" in s for s in plain["skipped"])
    assert not os.path.exists(os.path.join(fresh, "public",
                                           "sitemap.xml"))


def test_security_headers_hash_pins_csp(tmp_path):
    import agents as agents_mod
    site = os.path.join(str(tmp_path), "site")
    os.makedirs(os.path.join(site, "public"))
    page = ('<head><style is:inline>.a{color:red}</style>'
            '<script is:inline>var x = 1;</script></head>')
    out = agents_mod.write_security_headers(site, page)
    assert any("CSP" in a for a in out["applied"])
    headers = open(os.path.join(site, "public", "_headers")).read()
    assert "sha256-" in headers
    assert "default-src 'self'" in headers
    assert "frame-ancestors 'none'" in headers
    csp = agents_mod.csp_for_page('<script src="/x.js"></script>')
    assert "script-src 'self'" in csp and "sha256" not in csp


def test_light_mode_legible_for_accent_and_text():
    """Stitch separates tokens with hyphens; light mode must keep every
    token legible on near-white, not just the sampled pairs."""
    import agents as agents_mod
    palette = {
        "--c-background": "#101419", "--c-on-background": "#e0e2ea",
        "--c-surface": "#1c2025", "--c-on-surface": "#bac9cc",
        "--c-primary": "#c3f5ff", "--c-on-primary": "#00363d",
        "--c-secondary": "#7bd0ff", "--c-outline": "#849396",
    }
    built = agents_mod.build_palettes(palette)
    light = built["light"]
    # Surfaces near-white, text near-black: no washout.
    assert agents_mod.contrast_ratio(light["--c-background"],
                                    "#ffffff") < 1.4
    assert agents_mod.contrast_ratio(
        light["--c-on-surface"], light["--c-surface"]) >= 4.5
    # Accent dark enough to read as text on paper.
    assert agents_mod.contrast_ratio(light["--c-primary"],
                                    light["--c-background"]) >= 4.5
    # Every verified pair clears AA in both modes.
    for (mode, _s, _t), ratio in built["contrast"].items():
        assert ratio >= 4.5, (mode, ratio)
    # Dark palette is Stitch's own, untouched.
    assert built["dark"] == palette


def test_light_safety_css_targets_token_backgrounds():
    import agents as agents_mod
    css = agents_mod.light_mode_safety_css(
        ["--c-background", "--c-on-background", "--c-primary-container",
         "--c-primary", "--c-on-primary", "--c-outline"])
    assert "html:not(.dark) .bg-primary-container" in css
    assert "html:not(.dark) .bg-primary" in css
    # Text tokens and plain background are never targeted.
    assert "bg-on-primary" not in css and ".bg-background" not in css
    assert agents_mod.light_mode_safety_css([]) == ""
    # The rule reverses polarity in dark mode (no override there).
    assert ":not(.dark)" in css


def test_url_host_rejects_lookalike_hosts():
    """The allowlist must compare whole hosts. A substring test admits
    fonts.googleapis.com.evil.com, which is the defect CodeQL reports
    as incomplete URL substring sanitization."""
    import agents as agents_mod

    for hostile in ("https://fonts.googleapis.com.evil.com/x",
                    "https://evil-cdn.tailwindcss.com/x",
                    "https://evil.com/?fonts.googleapis.com",
                    "https://evil.com#fonts.googleapis.com",
                    "https://fonts.googleapis.com.evil.com",
                    "not a url", "", "///evil.com", "https://[::1"):
        host = agents_mod.url_host(hostile)
        assert host not in agents_mod.FONT_HOSTS, host
        assert host not in agents_mod.TAILWIND_CDN_HOSTS, host

    for legit in ("https://fonts.googleapis.com/css2?family=X",
                  "https://fonts.gstatic.com/a.woff2",
                  "https://cdn.tailwindcss.com?plugins=forms",
                  "HTTPS://FONTS.GOOGLEAPIS.COM/x"):
        host = agents_mod.url_host(legit)
        assert host in agents_mod.FONT_HOSTS or \
            host in agents_mod.TAILWIND_CDN_HOSTS, host

    # Malformed input yields no host rather than an exception.
    for bad in ("", "   ", "not a url", "javascript:alert(1)",
                "//evil.com/x", "https://[::1", "https://host:port/x"):
        assert agents_mod.url_host(bad) in ("", "host"), agents_mod.url_host(bad)


def test_head_allowlist_uses_hostnames():
    """A hostile <link>/<script> host must not survive the head filter."""
    import agents as agents_mod

    evil_link = agents_mod._keep_head_tag(
        "link", {"rel": "stylesheet",
                 "href": "https://fonts.googleapis.com.evil.com/x.css"})
    assert evil_link is False
    real_link = agents_mod._keep_head_tag(
        "link", {"rel": "stylesheet",
                 "href": "https://fonts.googleapis.com/css2?family=X"})
    assert real_link is True
    evil_script = agents_mod._keep_head_tag(
        "script", {"src": "https://cdn.tailwindcss.com.evil.com/x.js"})
    assert evil_script is False


class GetScreenOnlyStitch:
    """Stitch whose generate envelope omits htmlCode, and only
    get_screen carries it.

    This is the live shape that produced "design apply skipped (no
    Stitch HTML reference recorded)" — the journey shipped the bare
    Astro template instead of the Stitch design."""

    def __init__(self):
        self.calls = []
        self.downloads = []

    def call_tool(self, name, arguments):
        self.calls.append(name)
        if name == "create_project":
            return {"structuredContent": {"name": "projects/77"}}
        if name == "generate_screen_from_text":
            # No htmlCode, no nested screens: name only.
            return {"structuredContent": {"name": "projects/77/screens/s9",
                                          "title": "screen"}}
        if name == "get_screen":
            # The HTML reference lives here instead.
            return {"structuredContent": {
                "name": "projects/77/screens/s9",
                "screenshot": {"downloadUrl": "https://x/shot.png"},
                "htmlCode": {"downloadUrl": "https://x/page.html"},
            }}
        raise AssertionError(name)

    def download(self, url):
        self.downloads.append(url)
        with open(FIXTURE_HTML, "rb") as f:
            return f.read()

    def close(self):
        pass


def test_design_finds_html_reference_from_get_screen(tmp_path):
    """The HTML reference is mandatory and may arrive from either the
    generate envelope or get_screen."""
    import test_graph as tg
    from graph import (Deps, Deps as _D, build_graph, design_apply_stage,
                       design_stage)
    from langgraph.checkpoint.sqlite import SqliteSaver

    global FULL_BRIEF
    FULL_BRIEF = tg.FULL_BRIEF
    stitch = GetScreenOnlyStitch()
    deps = design_deps(tmp_path, stitch)
    initial = {"run_id": "gso", "brief": dict(FULL_BRIEF),
               "stage": "intake", "attempts": {}, "errors": []}
    out = design_stage(deps, initial)["design"]
    assert out["screen"] == "projects/77/screens/s9"
    assert out["html_url"] == "https://x/page.html", out
    assert out["screenshots"] == ["https://x/shot.png"]
    # The journey must now be able to apply it, so html_url is present
    # and the page is replaced with the Stitch markup.
    state = dict(initial, design=out)
    applied = design_apply_stage(deps, state)["design"]
    assert applied.get("applied"), applied
    assert any(a.startswith("faithful: index")
               for a in applied["applied"]), applied["applied"]
    assert stitch.downloads, "the HTML reference must be downloaded"


BRIEF = {"project_name": "demo", "purpose": "Demo.",
         "tone_of_voice": "Plain.", "primary_keywords": "demo",
         "geographic_scope": "Everywhere.", "non_goals": "None.",
         "ideal_customer_profile": "Ops.", "target_audience": "Ops.",
         "success_metric": "Done."}


def test_craft_design_prompt_uses_the_model_and_keeps_contract():
    import agents as agents_mod
    seen = []

    def llm(task, messages):
        seen.append((task, messages[0]["content"][:60]))
        return ("A confident municipal-infrastructure hero with a deep "
                "navy surface, a single accent of signal orange, and "
                "tabular numerics throughout, laid out as a sticky bar, "
                "a full-bleed headline, then three proof blocks. " * 3)

    prompt = agents_mod.craft_design_prompt(llm, BRIEF, "# Spec\nDemo.")
    assert seen[0][0] == "design"
    # Direction is composed, not dumped.
    assert "navy" in prompt
    assert "signal orange" in prompt
    # The delivery contract still rides along.
    assert "responsive HTML landing page" in prompt
    assert "Tailwind" in prompt
    assert "Do not return an image" in prompt
    # The spec reached the model.
    assert any("design" in s.lower() for _, s in seen)


def test_craft_design_prompt_falls_back_on_any_bad_output():
    import agents as agents_mod

    def bad(tag):
        return llm_raises

    def llm_raises(task, messages):
        raise RuntimeError("gateway down")

    fallback = agents_mod.craft_design_prompt(llm_raises, BRIEF, "")
    assert "responsive HTML landing page" in fallback
    assert "demo" in fallback

    for junk in ("", "short", '{"a":1}', "<p>html</p>", "x" * 9000,
                 "line\n" * 40):
        out = agents_mod.craft_design_prompt(
            lambda t, m, j=junk: j, BRIEF, "")
        assert out == fallback, junk[:20]

    # No client at all: still a usable prompt.
    assert agents_mod.craft_design_prompt(None, BRIEF, "") == fallback
