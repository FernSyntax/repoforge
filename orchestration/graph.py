"""Staged site-generation graph: the agentic journey with a review mesh.

intake → spec_draft → spec_critic → approve_gate → design →
project_model → generate → content → review → verify →
(repair ⟳ | report). Human interrupts pause for brief answers and for
spec approval; checkpoints persist every step in SQLite; every
autonomous loop carries an attempt bound. The LLM, MCP, and Stitch
surfaces arrive via Deps so tests run fully offline.

The review mesh (agents.py) fans out after content: accessibility,
SEO/AEO, security, performance/privacy, and i18n auditors inspect the
built tree read-only. Security findings block verify; the rest are
warnings recorded in agent_reports and surfaced in the final report.
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
    run_cmd: object = None  # (cmd: list, cwd: str, timeout: int) -> result with .returncode; None uses subprocess
    domain: str = ""  # production hostname (no scheme); empty keeps URLs relative
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
    reports = state.get("agent_reports") or {}
    if reports:
        summary = ", ".join(
            f"{name}={'pass' if rep.get('ok') else 'warn'}"
            for name, rep in sorted(reports.items()))
        lines.append(f"agents: {summary}")
    warnings = state.get("warnings") or []
    for w in warnings[:10]:
        lines.append(f"warning: {w}")
    if state.get("spec_critique"):
        lines.append("spec critique recorded")
    if isinstance(state.get("design"), dict) and state["design"].get("summary"):
        lines.append(f"design: {state['design']['summary']}")
    applied = (state.get("design") or {}).get("applied") or []
    if applied:
        joined = "; ".join(applied)
        if len(joined) > 300:
            joined = joined[:300].rsplit("; ", 1)[0] + "; …"
        lines.append(f"design applied: {joined}")
    if (state.get("design") or {}).get("faithful"):
        lines.append("faithful: index is the Stitch screen markup "
                     "(sanitized); body copy is Stitch-generated and "
                     "needs human review before publishing")
    if failure:
        lines += [f"failure: {failure}", "resumable: fix the cause and resume the thread"]
    else:
        lines += ["remaining: none", "action required: deploy dist/ from the generated site"]
    visual = state.get("visual", {}) or {}
    if visual.get("skipped"):
        lines += [f"visual audit: skipped ({visual['skipped']})"]
    else:
        findings = visual.get("findings", [])
        majors = [f for f in findings if f.get("severity") == "major"]
        lines += [f"visual audit: {len(findings)} finding(s), "
                  f"{len(majors)} major"]
        for note in [f.get("note", "") for f in majors][:5]:
            lines += [f"visual follow-up required: {note}"]
    return {"report": "\n".join(lines)}


def _unwrap(envelope):
    """Unwrap an MCP content envelope to its payload dict.

    Stitch (and other MCP servers) return
    {"content": [{"text": "<json>", ...}], "structuredContent": {...}}.
    Prefer structuredContent; fall back to parsing the first text part
    as JSON; fall back to the envelope itself when it already carries
    resource fields. Never raises: unparseable input yields {}.
    """
    if not isinstance(envelope, dict):
        return {}
    structured = envelope.get("structuredContent")
    if isinstance(structured, dict) and structured:
        return structured
    for part in envelope.get("content") or []:
        if isinstance(part, dict) and isinstance(part.get("text"), str):
            try:
                parsed = json.loads(part["text"])
            except ValueError:
                continue
            if isinstance(parsed, dict):
                return parsed
    return envelope


def _parse_findings(raw: str):
    """Validate a model findings payload. Returns (findings, error);
    error is empty on success."""
    import json as _json
    try:
        data = _json.loads(raw)
    except ValueError:
        return [], "not-json"
    if not isinstance(data, dict) or not isinstance(
            data.get("findings"), list):
        return [], "not-findings-shape"
    findings = []
    for item in data["findings"][:10]:
        if not isinstance(item, dict):
            return [], "finding-not-object"
        severity = item.get("severity")
        note = item.get("note", "")
        area = item.get("area", "")
        if severity not in ("info", "minor", "major"):
            return [], f"bad-severity: {severity!r}"
        if not isinstance(note, str) or not note.strip():
            return [], "blank-note"
        findings.append({"severity": severity, "area": str(area),
                         "note": note.strip()})
    return findings, ""


def _resource_name(envelope, kind: str) -> str:
    """First plausible resource identifier in a tool result, navigating
    defensively: Stitch nests resources differently per tool, and MCP
    wraps payloads in a content envelope (see _unwrap)."""
    payload = _unwrap(envelope)
    if not isinstance(payload, dict):
        return ""
    for key in ("name", kind, "project", "screen", "asset"):
        value = payload.get(key)
        if isinstance(value, str) and value:
            return value
        if isinstance(value, dict) and isinstance(value.get("name"), str):
            return value["name"]
    return ""


def _asset_urls(payload: dict) -> tuple[list[str], str]:
    """(screenshot URLs, htmlCode URL) from any screen-shaped payload.

    Stitch returns the same screen at several shapes: the generate
    envelope nests it under outputComponents[].design.screens[], while
    get_screen returns it directly. Both carry `screenshot` and
    `htmlCode` objects holding a downloadUrl, and some responses nest
    them under files/assets instead. One extractor serves both, so the
    HTML reference is found wherever Stitch put it.
    """
    if not isinstance(payload, dict):
        return [], ""

    def collect(source) -> list[str]:
        """downloadUrl values from a file reference, or from a map of
        them (the files/assets shape)."""
        if isinstance(source, dict):
            if source.get("downloadUrl"):
                return [str(source["downloadUrl"])]
            return [str(v["downloadUrl"]) for v in source.values()
                    if isinstance(v, dict) and v.get("downloadUrl")]
        if isinstance(source, list):
            out = []
            for item in source:
                if isinstance(item, dict) and item.get("downloadUrl"):
                    out.append(str(item["downloadUrl"]))
            return out
        return []

    shots = collect(payload.get("screenshot"))
    html_refs = collect(payload.get("htmlCode"))
    html_url = html_refs[0] if html_refs else ""
    for key in ("files", "assets"):
        items = payload.get(key)
        if not items:
            continue
        if isinstance(items, dict):
            items = list(items.values())
        for item in items:
            if not isinstance(item, dict):
                continue
            url = item.get("downloadUrl") or item.get("url")
            mime = str(item.get("mimeType", ""))
            role = str(item.get("role", "")).lower()
            if not url:
                continue
            if "html" in mime or role in ("htmlcode", "html"):
                if not html_url:
                    html_url = url
            elif "image" in mime or role in ("screenshot", "image"):
                shots.append(url)
    return shots, html_url


def _screen_record(generate_envelope) -> dict:
    """First screen in a generate_screen_from_text envelope.

    The live shape nests screens under
    structuredContent.outputComponents[].design.screens[] with inline
    screenshot/htmlCode download URLs and a designSystem. Older and
    stub shapes carry a top-level name instead. Returns {} when no
    screen is present so the caller falls back to get_screen.
    """
    payload = _unwrap(generate_envelope)
    if not isinstance(payload, dict):
        return {}
    for comp in payload.get("outputComponents") or []:
        design = (comp or {}).get("design") or {}
        screens = design.get("screens") or []
        if not screens or not isinstance(screens[0], dict):
            continue
        first = screens[0]
        record: dict = {}
        if isinstance(first.get("name"), str):
            record["screen"] = first["name"]
        shots, html_url = _asset_urls(first)
        if shots:
            record["shots"] = shots
        if html_url:
            record["html_url"] = html_url
        theme = first.get("theme")
        if isinstance(theme, dict) and theme:
            record["theme"] = {k: v for k, v in list(theme.items())[:16]
                               if isinstance(v, (str, int, float, dict))}
        tokens: dict = {}
        ds = ((first.get("designSystem") or {}).get("designSystem")
              or {})
        if isinstance(ds.get("displayName"), str):
            tokens["designSystem.displayName"] = ds["displayName"]
        theme = first.get("theme") or {}
        if isinstance(theme, dict):
            for k, v in list(theme.items())[:8]:
                if isinstance(v, (str, int, float)):
                    tokens[f"theme.{k}"] = v
        if tokens:
            record["tokens"] = tokens
        return record
    return {}


def _screenshot_urls(detail) -> list:
    try:
        from stitch import screenshot_urls as shots
        return shots(_unwrap(detail)) if isinstance(detail, dict) else []
    except Exception:
        return []


def _design_tokens(detail) -> dict:
    """First token-like mapping in a screen asset, flattened one level
    so nested color groups stay usable."""
    payload = _unwrap(detail)
    if not isinstance(payload, dict):
        return {}
    for key in ("designSystem", "design_system", "tokens", "theme"):
        value = payload.get(key)
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
    import agents as agents_mod
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
        # generate_screen_from_text requires the bare project ID
        # (without the `projects/` prefix); without it Stitch answers
        # "Requested entity was not found."
        project_id = (project_name.split("/", 1)[1]
                      if "/" in project_name else project_name)
        # A composed design brief, not a field dump: the model reads
        # the spec and asks for a real layout with a delivery
        # contract, because a bare hero prompt makes Stitch return an
        # image-only screen the journey cannot copy.
        prompt = agents_mod.craft_design_prompt(
            getattr(deps, "llm", None), state.get("brief", {}),
            state.get("spec", ""))
        screen = client.call_tool("generate_screen_from_text", {
            "projectId": project_id,
            "prompt": prompt,
            "deviceType": "DESKTOP",
        })
        record = {"project": project_name,
                  "screen": _resource_name(screen, "screen"),
                  "prompt": prompt[:400]}
        inline = _screen_record(screen)
        if inline.get("screen") and not record["screen"]:
            record["screen"] = inline["screen"]
        detail = {}
        if record["screen"]:
            try:
                detail = client.call_tool(
                    "get_screen", {"name": record["screen"]}) or {}
            except StitchError:
                detail = {}
        # The HTML reference is mandatory: it is the asset the journey
        # copies into the site. Stitch has returned it from either the
        # generate envelope or the get_screen detail depending on
        # response shape, so both are consulted before giving up.
        detail_shots, detail_html = _asset_urls(_unwrap(detail)
                                                if detail else {})
        shots = _screenshot_urls(detail)
        for url in detail_shots + list(inline.get("shots", [])):
            if url not in shots:
                shots.append(url)
        record["screenshots"] = shots
        html_url = inline.get("html_url") or detail_html
        if html_url:
            record["html_url"] = html_url
        if inline.get("theme"):
            record["theme"] = inline["theme"]
        tokens = dict(inline.get("tokens", {}))
        for k, v in _design_tokens(detail).items():
            tokens.setdefault(k, v)
        record["tokens"] = tokens
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


def design_apply_stage(deps, state: RunState) -> dict:
    """One bounded pass: download the Stitch screen HTML and apply its
    design (agents.apply_stitch_to_tree). Idempotent: a recorded
    application is not repeated. Reference artifacts (HTML + tokens)
    land in <workdir>/design/ for human review, outside the site tree.
    Best-effort: every failure path records a skip reason and returns
    to content; the review mesh still validates the tree."""
    import agents as agents_mod
    design = dict(state.get("design") or {})
    if design.get("applied"):
        return {"stage": "content"}
    # Design application is mandatory, not advisory: a configured Stitch
    # run that ships the bare template is a failed run, because the
    # client asked for the Stitch design and got the default theme.
    # Every path below therefore fails the run with an actionable
    # report instead of quietly keeping the template.
    factory = getattr(deps, "stitch_factory", None)
    if factory is None:
        return _fail(state, "design apply failed: Stitch is not configured "
                            "(set STITCH_API_KEY); the site would ship the "
                            "bare template instead of the approved design")
    if design.get("skipped"):
        return _fail(state, "design apply failed: design skipped ("
                     + str(design["skipped"]) + "); fix the cause and "
                     "resume the thread")
    html_url = design.get("html_url", "")
    if not html_url:
        return _fail(state, "design apply failed: Stitch recorded no HTML "
                            "reference for the generated screen; resume to "
                            "retry the design stage")
    try:
        client = factory()
    except Exception as e:
        return _fail(state, "design apply failed: client unavailable "
                            f"({type(e).__name__})")
    try:
        try:
            raw = client.download(html_url)
        finally:
            try:
                client.close()
            except Exception:
                pass
    except Exception as e:
        return _fail(state, "design apply failed: Stitch HTML unreachable "
                            f"({type(e).__name__}); resume to retry")
    if len(raw) > (2 << 20) + 1:
        return _fail(state, "design apply failed: Stitch HTML exceeds the "
                            "2MiB reference bound")
    try:
        html = raw.decode("utf-8", "replace")
    except Exception:
        return _fail(state, "design apply failed: Stitch HTML is not "
                            "decodable text")
    site_root = os.path.join(deps.allowed_root, deps.site_rel)
    try:
        outcome = agents_mod.apply_stitch_to_tree(
            site_root, html, design.get("theme"))
    except Exception as e:
        return _fail(state, "design apply failed: applying the Stitch "
                            f"design crashed ({type(e).__name__})")
    try:
        faithful = agents_mod.apply_faithful_page(
            site_root, html,
            str((state.get("brief") or {}).get("project_name", "site")))
    except Exception as e:
        faithful = {"applied": [],
                    "skipped": [f"faithful crashed: {type(e).__name__}"]}
    applied = list(outcome.get("applied", [])) + \
        list(faithful.get("applied", []))
    skipped = list(outcome.get("skipped", [])) + \
        list(faithful.get("skipped", []))
    design["applied"] = applied
    if any(a.startswith("faithful: index") for a in applied):
        design["faithful"] = True
    warnings = list(state.get("warnings", []))
    for note in outcome.get("skipped", [])[:8]:
        warnings.append(f"design: {note}")
    if design.get("faithful"):
        # Bake the Stitch theme to static CSS + self-hosted fonts so
        # the page renders identically offline: no CDN dependence at
        # view time. Failures keep the faithful CDN page (recorded).
        try:
            static = static_build_stage(deps, state, site_root, html,
                                        design)
        except Exception as e:
            static = {"applied": [],
                      "skipped": [f"static crashed: {type(e).__name__}"]}
        design["applied"] = applied + static.get("applied", [])
        skipped = skipped + static.get("skipped", [])
        for note in static.get("skipped", [])[:6]:
            warnings.append(f"design: {note}")
        # Production page pass: theme toggle, SEO head, link hygiene.
        # Each step is best-effort and recorded.
        brief = state.get("brief", {})
        domain = str(getattr(deps, "domain", "") or "")
        try:
            palettes = None
            if design.get("theme_palettes"):
                palettes = {"dark": design["theme_palettes"]["dark"],
                            "light": design["theme_palettes"]["light"]}
            elif design.get("theme"):
                raw_theme = design["theme"]
                only_colors = {
                    k: v for k, v in
                    (raw_theme.get("namedColors", {})
                     if isinstance(raw_theme, dict) else {}).items()
                    if isinstance(v, str)}
                if only_colors:
                    palettes = agents_mod.build_palettes(
                        {f"--c-{k}": v for k, v in only_colors.items()})
            if palettes:
                theme_out = agents_mod.inject_theme_toggle(
                    site_root, palettes)
                design["applied"] += theme_out["applied"]
                skipped += theme_out["skipped"]
        except Exception as e:
            skipped.append(f"theme crashed: {type(e).__name__}")
        try:
            seo_out = agents_mod.inject_seo_head(
                site_root, brief, domain, faithful=True)
            design["applied"] += seo_out["applied"]
            skipped += seo_out["skipped"]
        except Exception as e:
            skipped.append(f"seo-head crashed: {type(e).__name__}")
        try:
            links_out = agents_mod.harden_links(site_root)
            design["applied"] += links_out["applied"]
            skipped += links_out["skipped"]
        except Exception as e:
            skipped.append(f"links crashed: {type(e).__name__}")
    design["apply_skipped_notes"] = skipped
    try:
        ref_dir = os.path.join(deps.allowed_root, "design")
        os.makedirs(ref_dir, exist_ok=True)
        with open(os.path.join(ref_dir, "reference.html"), "w") as f:
            f.write(html[:1 << 20])
        with open(os.path.join(ref_dir, "tokens.json"), "w") as f:
            f.write(json.dumps(
                {"tokens": agents_mod.stitch_tokens(design.get("theme")),
                 "applied": design["applied"],
                 "skipped": design["apply_skipped_notes"],
                 "screen": design.get("screen", ""),
                 "project": design.get("project", "")},
                indent=2, sort_keys=True))
    except OSError:
        pass
    return {"stage": "content", "design": design,
            "warnings": warnings[:25]}


def _run_cmd(deps, cmd: list, cwd: str, timeout: int):
    """Run a build command, injectable for tests. Returns an object
    with .returncode; any exception means the step failed."""
    runner = getattr(deps, "run_cmd", None)
    if runner is not None:
        return runner(cmd, cwd, timeout)
    import subprocess
    return subprocess.run(cmd, cwd=cwd, timeout=timeout,
                          capture_output=True, text=True)


def _fetch_public(url: str, timeout: int = 30) -> bytes:
    """GET a public asset (Google Fonts) with a woff2-capable UA and a
    size cap. No credentials are ever attached."""
    import urllib.request
    if not url.startswith("https://"):
        raise ValueError("refusing non-https asset URL")
    req = urllib.request.Request(
        url, headers={"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) "
                                    "AppleWebKit/537.36 (KHTML, like Gecko) "
                                    "Chrome/120.0 Safari/537.36"},
        method="GET")
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.read(2 << 20)


def static_build_stage(deps, state: RunState, site_root: str,
                       html: str, design: dict) -> dict:
    """Compile the Stitch theme to static CSS and self-host fonts.

    Runs after the faithful page is written: npm-installs Tailwind v3
    (+ the CDN plugins the document asked for), builds
    src/styles/stitch.css from the Stitch theme, rewires the page to
    the static sheet, and downloads the fonts into public/. Any step
    may fail: the faithful CDN page stays untouched and the failure is
    recorded, so the run keeps Stitch fidelity either way. Returns an
    update dict for the design record plus warning lines.
    """
    import agents as agents_mod
    applied: list[str] = []
    skipped: list[str] = []
    extraction = agents_mod.extract_tailwind_config(html)
    if not extraction["present"]:
        return {"skipped": ["static: no Tailwind config in Stitch HTML "
                            "(CDN page kept)"]}
    unknown = [p for p in extraction["plugins"]
               if p not in agents_mod.TAILWIND_PLUGIN_PACKAGES]
    for name in unknown:
        skipped.append(f"static: unknown Tailwind plugin {name!r} ignored")
    themed = agents_mod.themed_tailwind_sources(
        site_root, extraction["config_js"], extraction["plugins"])
    if themed["error"]:
        return {"applied": applied,
                "skipped": skipped + [f"static: {themed['error']}"]}
    design["theme_mode"] = "dark-light"
    design["theme_contrast"] = themed["contrast"]
    design["theme_palettes"] = {"dark": themed["dark"],
                                "light": themed["light"]}
    applied.append("theme: CSS-variable palettes "
                   f"({len(themed['dark'])} tokens, contrast-verified)")
    packages = agents_mod.tailwind_npm_packages(extraction["plugins"])
    try:
        install = _run_cmd(
            deps, ["npm", "install", "--no-audit", "--no-fund",
                   "--save-dev"] + packages, site_root, 300)
        if getattr(install, "returncode", 1) != 0:
            raise RuntimeError("npm install failed")
        build = _run_cmd(
            deps, ["npx", "tailwindcss", "-i", "stitch.input.css",
                   "-o", os.path.join("src", "styles", "stitch.css"),
                   "--minify"], site_root, 180)
        if getattr(build, "returncode", 1) != 0:
            raise RuntimeError("tailwind build failed")
        css_path = os.path.join(site_root, "src", "styles", "stitch.css")
        if (not os.path.isfile(css_path) or
                os.path.getsize(css_path) == 0):
            raise RuntimeError("tailwind emitted no CSS")
    except Exception as e:
        return {"applied": applied,
                "skipped": skipped + [
                    f"static: build unavailable ({type(e).__name__}; "
                    "CDN page kept)"]}
    final = agents_mod.finalize_static_page(site_root)
    applied += final["applied"]
    skipped += final["skipped"]
    if not final["applied"]:
        return {"applied": applied, "skipped": skipped}
    try:
        import shutil
        for rel in ("node_modules", "tailwind.config.js",
                    "stitch.input.css"):
            target = os.path.join(site_root, rel)
            try:
                if os.path.isdir(target):
                    shutil.rmtree(target)
                elif os.path.isfile(target):
                    os.remove(target)
            except OSError:
                pass
        applied.append("static: build workspace cleaned "
                       "(node_modules, config, input removed)")
    except Exception:
        pass
    try:
        font_urls = agents_mod.collect_font_css_urls(html)
        if font_urls:
            package = agents_mod.fetch_font_package(
                font_urls, _fetch_public)
            total = sum(len(d) for d in package["files"].values())
            if total > (8 << 20):
                raise RuntimeError("font payload exceeds budget")
            stored = agents_mod.write_font_package(
                site_root, package["css"], package["files"])
            applied += stored["applied"]
            skipped += stored["skipped"]
            if stored["applied"]:
                page_path = os.path.join(
                    site_root, "src", "pages", "index.astro")
                with open(page_path) as f:
                    page = f.read()
                page = agents_mod.rewrite_fonts_local(page)
                with open(page_path, "w") as f:
                    f.write(page)
        else:
            skipped.append("static: no remote fonts to self-host")
    except Exception as e:
        skipped.append(f"static: fonts kept remote "
                       f"({type(e).__name__})")
    design["static_css"] = True
    return {"applied": applied, "skipped": skipped}
def visual_stage(deps, state: RunState) -> dict:
    """One bounded perceptual review: at most one screenshot, one model
    call, validated findings. Advisory only — majors are recorded for
    the human release decision, never block the run."""
    from llm import GatewayError, fetch_image, image_part
    import os as _os
    shots = ((state.get("design", {}) or {}).get("screenshots") or [])[:1]
    if not shots:
        return {"stage": "verify",
                "visual": {"skipped": "no-screenshots"}}
    try:
        image_b64, _mime = fetch_image(
            shots[0], api_key=_os.environ.get("STITCH_API_KEY", ""))
    except GatewayError as e:
        return {"stage": "verify",
                "visual": {"skipped": f"fetch-failed: {e.kind}"}}
    except Exception as e:
        return {"stage": "verify",
                "visual": {"skipped": f"fetch-failed: {type(e).__name__}"}}
    prompt = (
        "Review this landing-page render for accessibility and visual "
        "design as JSON {\"findings\": [{\"severity\": "
        "\"info|minor|major\", \"area\": \"contrast|hierarchy|spacing|"
        "legibility|focus\", \"note\": \"...\"}]}. "
        "Flag low-contrast text, unclear hierarchy, cramped spacing, "
        "and illegible sizes. No invented content; verdicts only."
    )
    try:
        raw = deps.llm("design-review", [
            {"role": "system",
             "content": "You review renders as strict JSON."},
            {"role": "user", "content": [
                {"type": "text", "text": prompt},
                image_part(image_b64)]},
        ])
    except GatewayError as e:
        return {"stage": "verify",
                "visual": {"skipped": f"review-failed: {e.kind}"}}
    except Exception as e:
        return {"stage": "verify",
                "visual": {"skipped": f"review-failed: {type(e).__name__}"}}
    findings, error = _parse_findings(raw)
    if error:
        return {"stage": "verify",
                "visual": {"skipped": f"invalid-findings: {error}"}}
    majors = [f for f in findings if f["severity"] == "major"]
    return {"stage": "verify",
            "visual": {"findings": findings,
                       "majors": [f["note"] for f in majors]}}


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
        last_error = ""
        for _ in range(2):
            try:
                spec = deps.llm("spec-drafting", [
                    {"role": "system", "content": "You write concise site specifications."},
                    {"role": "user", "content": prompt},
                ])
            except Exception as e:
                # Classified gateway failure (timeout/rate-limit/...):
                # retry once, then fail as a resumable report, never a
                # traceback. The credential never appears: callers
                # classify and redact before raising.
                last_error = f"{type(e).__name__}: {e}"
                continue
            if spec and spec.strip():
                return {"spec": spec, "spec_digest": digest(spec),
                        "stage": "spec_critic"}
        if last_error:
            return _fail(state, f"spec drafting failed: {last_error} "
                                "(gateway transient: resume the thread to retry)")
        return _fail(state, "spec drafting returned empty output twice")

    def spec_critic(state: RunState) -> dict:
        # Advisory single pass: records review notes for the approver,
        # never blocks the gate. Bounded by construction (one LLM call,
        # empty output tolerated).
        try:
            import agents as agents_mod
            critique = agents_mod.spec_critique(
                deps.llm, state.get("spec", ""), state.get("brief", {}))
        except Exception:
            critique = ""
        return {"spec_critique": critique, "stage": "approve_gate"}

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
            return {"stage": "design_apply"}
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
        return {"generated": True, "stage": "design_apply"}

    def design_apply(state: RunState) -> dict:
        # Copy the Stitch screen's DESIGN into the generated tree:
        # tokens, header nav, footer nav (see agents.apply_stitch_to_tree).
        # Best-effort by contract: any failure becomes a recorded skip
        # plus warnings, never a run failure. The review mesh validates
        # the result before verify.
        if state.get("failed"):
            return {"stage": "content"}
        return design_apply_stage(deps, state)

    def production(state: RunState) -> dict:
        # Write production files after the page is final (content has
        # run): robots/sitemap/og.svg plus hash-pinned _headers over
        # the exact shipped markup. Deterministic rewrites; safe to
        # re-run on repair loops. Best-effort, recorded.
        if state.get("failed"):
            return {"stage": "review"}
        import agents as agents_mod
        site_root = os.path.join(deps.allowed_root, deps.site_rel)
        brief = state.get("brief", {})
        domain = str(getattr(deps, "domain", "") or "")
        design = dict(state.get("design") or {})
        applied = list(design.get("production_applied", []))
        skipped: list[str] = []
        warnings = list(state.get("warnings", []))
        try:
            files = agents_mod.write_seo_files(site_root, brief, domain)
            for note in files["applied"]:
                if note not in applied:
                    applied.append(note)
            skipped += files["skipped"]
        except Exception as e:
            skipped.append(f"production files crashed: {type(e).__name__}")
        try:
            with open(os.path.join(site_root, "src", "pages",
                                   "index.astro")) as f:
                final_page = f.read()
            headers = agents_mod.write_security_headers(site_root,
                                                        final_page)
            for note in headers["applied"]:
                if note not in applied:
                    applied.append(note)
            skipped += headers["skipped"]
        except Exception as e:
            skipped.append(f"_headers crashed: {type(e).__name__}")
        design["production_applied"] = applied
        for note in skipped[:8]:
            warnings.append(note)
        return {"stage": "review", "design": design,
                "warnings": warnings[:25]}

    def content(state: RunState) -> dict:
        attempts = dict(state.get("attempts", {}))
        if state.get("failed"):
            return {"stage": "production"}
        if state.get("content_done"):
            # Already applied: a repair cycle must not rewrite it, and
            # the placeholder contract no longer holds afterwards.
            # Production files are rewritten deterministically.
            return {"stage": "production"}
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
        if (state.get("design") or {}).get("faithful"):
            # Faithful mode: the index IS the Stitch screen markup, so
            # the template FAQ contract does not apply. Body copy is
            # Stitch-generated; flag the human-review obligation and
            # release the content pass without an LLM call.
            warnings = list(state.get("warnings", [])) + [
                "body copy is Stitch-generated: review metrics, "
                "certifications, and URLs before publishing"]
            return {"content_done": True, "stage": "production",
                    "warnings": warnings[:25]}
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
        try:
            raw = deps.llm("documentation", [
                {"role": "system", "content": "You write honest marketing copy as JSON."},
                {"role": "user", "content": prompt},
            ])
        except Exception as e:
            attempts["content"] = used + 1
            if used + 1 >= deps.max_content_attempts:
                return dict(_fail(
                    state, f"content gateway failed: {type(e).__name__}: {e} "
                           "(transient: fix connectivity and resume)"),
                    attempts=attempts)
            return {"attempts": attempts, "stage": "content"}
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
                "stage": "production"}

    def review(state: RunState) -> dict:
        # Fan-out of read-only auditors (agents.py). Security and link
        # findings block verify; production quality (SEO/AEO/GEO/
        # images) advises. Never mutates the tree; never fails the run.
        if state.get("failed"):
            return {"stage": "verify"}
        try:
            import agents as agents_mod
            root = os.path.join(deps.allowed_root, deps.site_rel)
            reports = agents_mod.run_review_suite(
                root, state.get("brief", {}),
                domain=str(getattr(deps, "domain", "") or ""))
        except Exception as e:
            return {"agent_reports": {},
                    "warnings": [f"review suite crashed: {type(e).__name__}"],
                    "stage": "verify"}
        warnings: list[str] = []
        for name, rep in reports.items():
            for issue in rep.get("issues", [])[:5]:
                if name in ("security", "links"):
                    continue  # blocking: handled in verify
                warnings.append(issue)
        # ICP fit is advisory but always visible in the report.
        icp = reports.get("icp") or {}
        if icp:
            warnings.append(
                f"icp: match {icp.get('score', 0)}% "
                f"(missing: {', '.join((icp.get('missing') or [])[:5]) or 'none'})"
                f"; fabricated: {', '.join((icp.get('forbidden') or [])[:3]) or 'none'}")
        prior = list(state.get("warnings", []))
        merged = (prior + warnings)[:25]
        return {"agent_reports": reports, "warnings": merged,
                "stage": "visual_audit"}

    def visual_audit(state: RunState) -> dict:
        # Advisory perceptual review of at most one Stitch render: majors
        # are recorded for the human release decision, nothing here
        # blocks the run, and every absence is a recorded skip.
        return visual_stage(deps, state)

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
            if not (state.get("design") or {}).get("faithful"):
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
        for issue in (state.get("agent_reports") or {}).get(
                "security", {}).get("issues", []):
            problems.append(issue)
        # Link hygiene is blocking too: a javascript: URL or a remote
        # form post is a security defect, not a style opinion.
        for issue in (state.get("agent_reports") or {}).get(
                "links", {}).get("issues", []):
            problems.append(issue)
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
        domain = str(getattr(deps, "domain", "") or "")
        if domain:
            out["report"] += (f"\ndomain: {domain} (canonical, "
                              f"sitemap, social cards)")
        applied = (state.get("design") or {}).get("production_applied") \
            or []
        if applied:
            joined = "; ".join(applied)
            if len(joined) > 240:
                joined = joined[:240].rsplit("; ", 1)[0] + "; …"
            out["report"] += f"\nproduction: {joined}"
        icp = (state.get("agent_reports") or {}).get("icp") or {}
        if icp:
            out["report"] += (
                f"\nicp: match {icp.get('score', 0)}% — covered "
                f"{len(icp.get('covered', []))} terms, missing "
                f"{len(icp.get('missing', []))}, fabrication flags: "
                + (", ".join((icp.get('forbidden') or [])[:4]) or "none"))
        out.update({"done": True, "stage": "done"})
        return out

    graph = StateGraph(RunState)
    graph.add_node("intake", intake)
    graph.add_node("spec_draft", spec_draft)
    graph.add_node("spec_critic", spec_critic)
    graph.add_node("approve_gate", approve_gate)
    graph.add_node("design", design)
    graph.add_node("project_model", project_model)
    graph.add_node("generate", generate)
    graph.add_node("design_apply", design_apply)
    graph.add_node("content", content)
    graph.add_node("production", production)
    graph.add_node("review", review)
    graph.add_node("visual_audit", visual_audit)
    graph.add_node("verify", verify)
    graph.add_node("report", report)
    graph.add_edge(START, "intake")
    graph.add_edge("intake", "spec_draft")
    graph.add_edge("spec_draft", "spec_critic")
    graph.add_edge("spec_critic", "approve_gate")
    graph.add_edge("approve_gate", "design")
    graph.add_edge("design", "project_model")
    graph.add_edge("project_model", "generate")
    graph.add_edge("generate", "design_apply")
    graph.add_edge("design_apply", "content")
    graph.add_edge("content", "production")
    graph.add_edge("production", "review")
    graph.add_edge("review", "visual_audit")
    graph.add_edge("visual_audit", "verify")
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
