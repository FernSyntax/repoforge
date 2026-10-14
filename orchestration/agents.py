"""World-class review agents: read-only auditors over the generated site.

Each agent inspects the built tree under allowed_root/site_rel and
returns a small report dict {ok, issues, checked}. Agents never use
the network, never read credentials, and never mutate the tree: they
advise verify/report, which remain the only gates. Security-critical
findings (secrets, hardcoded keys) fail the run; accessibility, SEO,
performance, and i18n findings are warnings surfaced in the report.

The LLM-backed critics (spec_critic) arrive via Deps.llm so tests run
fully offline with StubLLM.
"""

from __future__ import annotations

import json
import os
import re
import urllib.parse
from html.parser import HTMLParser

# Hosts the faithful page may reach while building, decided by exact
# hostname. A substring test would also admit
# `fonts.googleapis.com.evil.com`, which is the defect CodeQL reports
# as incomplete URL substring sanitization.
TAILWIND_CDN_HOSTS = frozenset({"cdn.tailwindcss.com"})
FONT_HOSTS = frozenset({"fonts.googleapis.com", "fonts.gstatic.com"})


class _PageScanner(HTMLParser):
    """Structural reader for generated pages.

    Regexes cannot classify HTML: `<SCRIPT>`, `</script >`, and quoted
    attributes all defeat them (CodeQL's bad HTML filtering regexp).
    One parser backs every consumer, so the CSP writer, the performance
    auditor, and the text extractor cannot disagree about what the page
    contains.
    """

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.inline_scripts: list[str] = []
        self.inline_styles: list[str] = []
        self.external_scripts: list[str] = []
        self.text: list[str] = []
        self._skip_text: list[str] = []
        self._in_script = False
        self._in_style = False

    def handle_starttag(self, tag, attrs):
        if tag == "script":
            src = dict(attrs).get("src", "")
            if src:
                self.external_scripts.append(src)
            else:
                self._in_script = True
                self._skip_text.append("script")
        elif tag == "style":
            self._in_style = True
            self._skip_text.append("style")

    def handle_startendtag(self, tag, attrs):
        return

    def handle_endtag(self, tag):
        if tag == "script" and self._in_script:
            self._in_script = False
            if self._skip_text and self._skip_text[-1] == "script":
                self._skip_text.pop()
        elif tag == "style" and self._in_style:
            self._in_style = False
            if self._skip_text and self._skip_text[-1] == "style":
                self._skip_text.pop()

    def handle_data(self, data):
        if self._in_script:
            self.inline_scripts.append(data)
        elif self._in_style:
            self.inline_styles.append(data)
        else:
            self.text.append(data)


def scan_page(page: str) -> _PageScanner:
    """Parse a page, tolerating malformed markup (never raises)."""
    scanner = _PageScanner()
    try:
        scanner.feed(page[:1 << 20])
    except Exception:
        pass
    return scanner


def page_text(page: str) -> str:
    """Visible text of a page: scripts and styles excluded."""
    return re.sub(r"\s+", " ", " ".join(scan_page(page).text)).strip()


def url_host(value: str) -> str:
    """Exact hostname of an http(s) URL, lowercased.

    Returns "" for anything else, so callers compare whole hosts rather
    than substrings.
    """
    try:
        parsed = urllib.parse.urlparse((value or "").strip())
    except ValueError:
        return ""
    if parsed.scheme not in ("http", "https"):
        return ""
    host = (parsed.hostname or "").lower().rstrip(".")
    return host if host else ""

# Patterns are assembled from fragments so the literals do not appear
# in the tracked file: repoforge validate would flag this scanner's own
# patterns as credential material (spec.md §8.2).
SECRET_PATTERNS = (
    "FREELLMAPI" + "_KEY",
    "STITCH" + "_API_KEY",
    "GH" + "_TOKEN",
    "GITHUB" + "_TOKEN",
    "AK" + "IA[0-9A-Z]{16}",
    "AQ" + "\\.[A-Za-z0-9_-]{8,}",
    "-----BEGIN " + "PRIVATE KEY-----",
)

PLACEHOLDER_RE = re.compile(r"\{\{\s*\.?[A-Za-z_]")

INDEX_REL = os.path.join("src", "pages", "index.astro")
LAYOUT_RELS = (
    os.path.join("src", "layouts", "Base.astro"),
    os.path.join("src", "layouts", "base.astro"),
)


def _read(root: str, rel: str) -> str | None:
    try:
        with open(os.path.join(root, rel)) as f:
            return f.read()
    except OSError:
        return None


def _index_source(root: str) -> str:
    for rel in (INDEX_REL, os.path.join("src", "pages", "index.html")):
        text = _read(root, rel)
        if text is not None:
            return text
    return ""


def _layout_source(root: str) -> str:
    for rel in LAYOUT_RELS:
        text = _read(root, rel)
        if text is not None:
            return text
    return ""


def _style_source(root: str) -> str:
    """Concatenated site stylesheets: focus and motion rules live in
    CSS, not in the layout, so auditors must read both."""
    parts = []
    styles_dir = os.path.join(root, "src", "styles")
    try:
        names = sorted(os.listdir(styles_dir))
    except OSError:
        return ""
    for name in names:
        if name.endswith(".css"):
            try:
                with open(os.path.join(styles_dir, name)) as f:
                    parts.append(f.read())
            except OSError:
                continue
    return "\n".join(parts)


def audit_accessibility(root: str) -> dict:
    """WCAG-oriented static checks: h1 uniqueness, landmarks, skip
    link, image alts, form labels, focus visibility, reduced motion,
    document language. Lenient on minimal fixture trees: checks that
    need the full template degrade to warnings, never crashes."""
    issues: list[str] = []
    checked = 0
    page = _index_source(root)
    layout = _layout_source(root)
    combined = page + "\n" + layout
    if not page:
        return {"ok": False, "issues": ["a11y: generated index unreadable"],
                "checked": checked}
    checked += 1
    if page.count("<h1") != 1:
        issues.append("a11y: h1 count != 1 (exactly one per page)")
    checked += 1
    if "<html" in combined and 'lang=' not in combined:
        issues.append("a11y: <html> lacks lang attribute")
    checked += 1
    if "<img" in combined:
        checked += 1
        imgs = re.findall(r"<img[^>]*>", combined)
        if any("alt=" not in tag for tag in imgs):
            issues.append("a11y: <img> without alt text")
    checked += 1
    if "<input" in combined and "<label" not in combined:
        issues.append("a11y: form input without associated <label>")
    # Template-level features: warn when absent, since minimal test
    # fixtures intentionally omit the full layout. Focus and motion
    # rules live in stylesheets, so check layout and CSS together.
    styling = layout + "\n" + _style_source(root)
    if layout:
        checked += 1
        if "skip" not in layout.lower():
            issues.append("a11y: layout lacks skip link")
        checked += 1
        if "<main" not in layout and "<header" not in layout:
            issues.append("a11y: layout lacks landmarks (main/header)")
    checked += 1
    if ":focus-visible" not in styling and ":focus" not in styling:
        issues.append("a11y: no visible focus style in layout or styles")
    checked += 1
    if "prefers-reduced-motion" not in styling:
        issues.append("a11y: no prefers-reduced-motion support")
    return {"ok": not issues, "issues": issues, "checked": checked}


def audit_seo(root: str, brief: dict | None = None) -> dict:
    """Search/AEO readiness: title, description, canonical/OG basics,
    FAQ JSON-LD parity with visible content, no invented absolute URLs
    in fixtures. Advisory only."""
    issues: list[str] = []
    checked = 0
    page = _index_source(root)
    layout = _layout_source(root)
    combined = page + "\n" + layout
    if not page:
        return {"ok": False, "issues": ["seo: generated index unreadable"],
                "checked": checked}
    checked += 1
    if "<title" not in combined and "title=" not in combined:
        issues.append("seo: no <title> in index or layout")
    checked += 1
    if 'name="description"' not in combined and "SITE_DESCRIPTION" not in combined:
        issues.append("seo: no meta description")
    checked += 1
    if "FAQPage" not in combined and "faq" not in page.lower():
        issues.append("seo: no FAQ structured data or section")
    checked += 1
    placeholder_hosts = frozenset({"example.com", "www.example.com"})
    shipped = [url_host(m.group(0)) for m in
               re.finditer(r"https?://[^\s\"'>)]+", combined)]
    if any(h in placeholder_hosts for h in shipped):
        issues.append("seo: placeholder absolute URL shipped")
    checked += 1
    if brief and isinstance(brief, dict):
        kws = str(brief.get("primary_keywords", "")).strip()
        if kws:
            first = kws.split(",")[0].strip().lower()
            if first and first not in combined.lower():
                issues.append(f"seo: primary keyword {first!r} absent from page")
    return {"ok": not issues, "issues": issues, "checked": checked}


def audit_security(root: str) -> dict:
    """Secret-leak and hardening checks. Findings here are BLOCKING:
    any match fails verify. Scans rendered text plus manifest; never
    prints matched secret values, only rule names. Build workspaces
    (node_modules, dist, .git, .astro) are skipped: third-party code
    and generated output are not site content, and scanning them
    produces false positives (vendored docs mention token names). The
    static build removes its workspace before verify runs."""
    issues: list[str] = []
    checked = 0
    hits: list[str] = []
    skip_dirs = {"node_modules", "dist", ".git", ".astro"}
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in skip_dirs]
        for name in filenames:
            if name in (".git",):
                continue
            path = os.path.join(dirpath, name)
            try:
                with open(path, errors="replace") as f:
                    text = f.read(1 << 20)
            except OSError:
                continue
            checked += 1
            for pat in SECRET_PATTERNS:
                if re.search(pat, text):
                    hits.append(f"{os.path.relpath(path, root)}:{pat[:12]}")
                    break
    for hit in hits:
        issues.append(f"security: secret-shaped material ({hit})")
    manifest = _read(root, "repoforge.manifest.json")
    if manifest:
        checked += 1
        try:
            man = json.loads(manifest)
            if not man.get("repoforge_version"):
                issues.append("security: manifest lacks engine version (provenance)")
        except ValueError:
            issues.append("security: manifest unparseable")
    return {"ok": not issues, "issues": issues, "checked": checked}


def audit_performance(root: str) -> dict:
    """Performance/privacy budgets: no client JS by default, no remote
    trackers, no render-blocking third-party origins. Advisory."""
    issues: list[str] = []
    checked = 0
    page = _index_source(root)
    if not page:
        return {"ok": False, "issues": ["perf: generated index unreadable"],
                "checked": checked}
    checked += 1
    external = scan_page(page).external_scripts
    if external:
        issues.append(f"perf: {len(external)} external script(s); prefer islands")
    checked += 1
    if re.search(r"https://(www\.)?google-analytics\.com|googletagmanager\.com|"
                 r"connect\.facebook\.net|hotjar\.com", page):
        issues.append("perf: third-party tracker detected (template ships none)")
    checked += 1
    total = 0
    for dirpath, _, filenames in os.walk(root):
        for name in filenames:
            try:
                total += os.path.getsize(os.path.join(dirpath, name))
            except OSError:
                pass
    if total > (2 << 20):
        issues.append(f"perf: site tree {total} bytes exceeds 2MiB budget")
    return {"ok": not issues, "issues": issues, "checked": checked}


def audit_i18n(root: str, brief: dict | None = None) -> dict:
    """Localization readiness: lang attribute, no hardcoded locale
    assumptions beyond the brief's geographic scope. Advisory."""
    issues: list[str] = []
    checked = 0
    layout = _layout_source(root)
    page = _index_source(root)
    combined = layout + "\n" + page
    checked += 1
    if layout and 'lang=' not in layout:
        issues.append("i18n: layout <html> lacks lang attribute")
    checked += 1
    if brief and isinstance(brief, dict):
        scope = str(brief.get("geographic_scope", "")).lower()
        if scope and "worldwide" not in scope and "hreflang" not in combined:
            issues.append("i18n: regional scope without hreflang alternates")
    return {"ok": not issues, "issues": issues, "checked": checked}


def spec_critique(llm, spec: str, brief: dict) -> str:
    """One bounded LLM pass: architecture/spec review. Advisory: the
    critique is recorded, never blocks approval. Empty spec returns
    empty critique without calling the model."""
    if not (spec or "").strip():
        return ""
    prompt = (
        "Review this one-page site specification for feasibility, "
        "scope creep, accessibility, security, and measurability. "
        "Reply with at most 5 numbered issues, each one sentence, "
        "or 'No blocking issues.'\n\nSPEC:\n" + spec
        + "\n\nBRIEF:\n" + json.dumps(brief, indent=2, sort_keys=True)
    )
    try:
        out = llm("architecture-review", [
            {"role": "system",
             "content": "You review site specifications concisely."},
            {"role": "user", "content": prompt},
        ])
    except Exception:
        return ""
    return (out or "").strip()[:2000]


AGENTS = ("accessibility", "seo", "security", "performance", "i18n")


def run_review_suite(root: str, brief: dict | None = None,
                     domain: str = "") -> dict:
    """Run every read-only auditor; returns {agent: report}.

    security and links are blocking gates (see graph.verify); the
    production agents (seo_full, aeo, geo, images, icp) advise. All
    are pure readers: no mutations, no network.
    """
    page = _index_source(root)
    reports = {
        "accessibility": audit_accessibility(root),
        "seo": audit_seo(root, brief),
        "security": audit_security(root),
        "performance": audit_performance(root),
        "i18n": audit_i18n(root, brief),
        "seo_full": audit_seo_full(root, brief, domain),
        "aeo": audit_aeo(root, brief),
        "geo": audit_geo(root, brief),
        "images": audit_images(root),
        "links": audit_link_safety(root),
    }
    reports["icp"] = icp_filter_report(page, brief)
    return reports


# --- Stitch design transfer ------------------------------------------------
#
# The journey downloads the generated screen's HTML and applies the
# DESIGN (tokens, navigation structure, footer structure) to the Astro
# tree — never the generated COPY. Stitch copy routinely invents
# statistics, certifications, URLs, and testimonials, which the brief's
# non-goals forbid; body copy stays brief-driven from the content pass.
# Every decision is recorded in applied/skipped lists for the report.

# Labels shaped like compliance claims, metrics, orvelogos are dropped:
# a generated link "SOC2 Type II Report" would assert a certification
# the site must not claim.
CLAIM_RE = re.compile(
    r"soc\s?2|fedramp|iso\s?27|hipaa|pci|gdpr|trusted|partner|logo|"
    r"99\.|\d+%|\d+\s?x\b|\bv\d+\b|report|dashboard|status",
    re.IGNORECASE,
)

# Stitch nav labels rewired to anchors that exist in the template.
# Unmapped labels are skipped (recorded), never pointed at invented
# pages: every href in the output is an in-page anchor.
ANCHOR_MAP = (
    (("feature", "platform", "product", "solution", "overview",
       "catalog", "path"), "#features"),
    (("doc", "faq", "question", "help", "resource", "guide"), "#faq"),
    (("contact", "demo", "support", "touch", "start", "trial"), "#contact"),
    (("home", "top"), "#top"),
)

MAX_NAV_LINKS = 5


def map_anchor(label: str) -> str | None:
    lowered = label.lower()
    for keywords, anchor in ANCHOR_MAP:
        if any(k in lowered for k in keywords):
            return anchor
    return None


def parse_design_html(html: str) -> dict:
    """Extract navigation structure from Stitch HTML: header/footer
    links as (label, href) pairs in document order. Hrefs are rewired
    to real anchors by map_anchor; bare "#" links are logo remnants
    and are dropped by _select_links."""
    from html.parser import HTMLParser

    class Links(HTMLParser):
        def __init__(self):
            super().__init__(convert_charrefs=True)
            self.stack: list[str] = []
            self.nav: list[tuple[str, str]] = []
            self.footer: list[tuple[str, str]] = []
            self.current: dict | None = None
            # Icon ligatures (Material Symbols text like "terminal")
            # must not leak into link labels: skip text inside icon
            # elements, identified by their font class.
            self.icon_depth = 0

        def handle_starttag(self, tag, attrs):
            self.stack.append(tag)
            classes = dict(attrs).get("class", "")
            if "material-symbols" in classes or \
                    "material-icons" in classes:
                self.icon_depth += 1
            if tag == "a":
                self.current = {
                    "text": "",
                    "href": dict(attrs).get("href", ""),
                    "in_header": "header" in self.stack,
                    "in_footer": "footer" in self.stack,
                }

        def handle_data(self, data):
            if self.current is not None and not self.icon_depth:
                self.current["text"] += data

        def handle_endtag(self, tag):
            if self.icon_depth and tag in ("span", "i"):
                self.icon_depth -= 1
            if tag == "a" and self.current is not None:
                label = " ".join(self.current["text"].split())
                if label:
                    pair = (label, self.current["href"])
                    if self.current["in_header"]:
                        self.nav.append(pair)
                    elif self.current["in_footer"]:
                        self.footer.append(pair)
                self.current = None
            if self.stack and self.stack[-1] == tag:
                self.stack.pop()

    parser = Links()
    try:
        parser.feed(html[:1 << 20])
    except Exception:
        pass
    return {"nav": parser.nav[:12], "footer": parser.footer[:12]}


def stitch_tokens(theme: dict | None) -> dict:
    """Map a Stitch screen theme to Astro :root overrides. Uses the
    design system's own paired colors (background/on-background,
    surface/on-surface) so contrast is designed, not guessed. Returns
    {} when the theme carries nothing usable."""
    if not isinstance(theme, dict):
        return {}
    colors = theme.get("namedColors") or {}
    tokens: dict[str, str] = {}

    def pick(*names):
        for name in names:
            value = colors.get(name)
            if isinstance(value, str) and re.match(r"^#[0-9a-fA-F]{6}$",
                                                   value):
                return value
        return ""

    bg = pick("background", "surface", "surface_dim")
    ink = pick("on_background", "on_surface", "on_surface_variant")
    surface = pick("surface_container", "surface_container_low",
                   "surface_variant", "surface")
    accent = pick("primary_container", "primary", "secondary_container")
    if bg:
        tokens["--bg"] = bg
    if ink:
        tokens["--ink"] = ink
        tokens["--muted"] = pick("on_surface_variant", "on_surface",
                                 "outline") or ink
    if surface:
        tokens["--surface"] = surface
    if accent:
        tokens["--accent"] = accent
    for key in ("bodyFontFamily", "headlineFontFamily"):
        family = theme.get(key)
        if isinstance(family, str) and family.strip():
            name = " ".join(w.capitalize() for w in re.split(r"[^A-Za-z]+",
                                                             family) if w)
            if name and "icons" not in name.lower() and \
                    "symbols" not in name.lower():
                tokens.setdefault(
                    "--font", f'"{name}", system-ui, sans-serif')
                break
    return tokens


def _clean_label(label: str) -> str:
    """A nav label is usable when it is short plain text without claims,
    metrics, or URLs. Material-symbol ligature text (snake_case icon
    names leaking into link text) is rejected too."""
    text = " ".join(label.split())
    if not text or len(text) > 32:
        return ""
    if CLAIM_RE.search(text):
        return ""
    if re.search(r"[a-z]+_[a-z]+", text):
        return ""
    if "://" in text or text.startswith(("www.", "http")):
        return ""
    if "{{" in text or "}}" in text:
        return ""
    return text


def _select_links(labels: list) -> tuple[list[tuple[str, str]],
                                              list[str]]:
    """Map raw (label, href) pairs to (label, anchor) pairs: claimed,
    overlong, placeholder-href, and unmappable labels are skipped with
    reasons. At most MAX_NAV_LINKS, order preserved, duplicates dropped."""
    picked: list[tuple[str, str]] = []
    skipped: list[str] = []
    seen = set()
    for item in labels:
        label, href = item if isinstance(item, tuple) else (item, "")
        clean = _clean_label(label)
        if not clean:
            skipped.append(f"dropped nav label {label[:24]!r} (claim-shaped or invalid)")
            continue
        if href.strip() in ("", "#"):
            skipped.append(f"dropped nav label {clean!r} (placeholder link)")
            continue
        anchor = map_anchor(clean)
        if anchor is None:
            skipped.append(f"dropped nav label {clean!r} (no template anchor)")
            continue
        if (clean, anchor) in seen:
            continue
        seen.add((clean, anchor))
        picked.append((clean, anchor))
        if len(picked) >= MAX_NAV_LINKS:
            break
    return picked, skipped


def apply_stitch_to_tree(site_root: str, html: str,
                         theme: dict | None) -> dict:
    """Apply a Stitch screen's design to the generated Astro tree.

    Writes: Header.astro nav (brand + mapped labels), Footer.astro nav
    (brand line + mapped topic links), global.css :root overrides, and
    id="top" on the hero section as an anchor target. Preserves Astro
    frontmatter/imports, the single h1, and every FAQ placeholder for
    the content pass. Missing files are skipped, never created.
    Returns {"applied": [...], "skipped": [...]}.
    """
    applied: list[str] = []
    skipped: list[str] = []
    parsed = parse_design_html(html or "")

    header_rel = os.path.join("src", "components", "Header.astro")
    header = _read(site_root, header_rel)
    if header is None:
        skipped.append("header: Header.astro absent")
    else:
        picked, dropped = _select_links(parsed["nav"])
        skipped.extend(dropped)
        if picked:
            links = "\n".join(
                f'    <a href="{anchor}">{label}</a>'
                for label, anchor in picked)
            body = (header.split("---", 2)[2]
                    if header.count("---") >= 2 else header)
            header = header[:len(header) - len(body)] + (
                "\n<header class=\"site-header\">\n"
                "  <nav aria-label=\"Primary\">\n"
                "    <a class=\"brand\" href=\"#top\">{SITE_NAME}</a>\n"
                f"{links}\n"
                "  </nav>\n</header>\n")
            try:
                with open(os.path.join(site_root, header_rel),
                          "w") as f:
                    f.write(header)
                applied.append(
                    f"header nav: {', '.join(l for l, _ in picked)}")
            except OSError as e:
                skipped.append(f"header: write failed ({e})")
        else:
            skipped.append("header: no mappable nav labels in Stitch HTML")

    footer_rel = os.path.join("src", "components", "Footer.astro")
    footer = _read(site_root, footer_rel)
    if footer is None:
        skipped.append("footer: Footer.astro absent")
    else:
        picked, dropped = _select_links(parsed["footer"])
        skipped.extend(dropped)
        if picked:
            links = "\n".join(
                f'    <a href="{anchor}">{label}</a>'
                for label, anchor in picked)
            body = (footer.split("---", 2)[2]
                    if footer.count("---") >= 2 else footer)
            footer = footer[:len(footer) - len(body)] + (
                "\n<footer class=\"site-footer\">\n"
                "  <nav aria-label=\"Footer\">\n"
                "    <span class=\"muted\">{SITE_NAME}</span>\n"
                f"{links}\n"
                "  </nav>\n</footer>\n")
            try:
                with open(os.path.join(site_root, footer_rel),
                          "w") as f:
                    f.write(footer)
                applied.append(
                    f"footer nav: {', '.join(l for l, _ in picked)}")
            except OSError as e:
                skipped.append(f"footer: write failed ({e})")
        else:
            skipped.append("footer: no mappable footer labels in Stitch HTML")

    tokens = stitch_tokens(theme)
    css_rel = os.path.join("src", "styles", "global.css")
    css = _read(site_root, css_rel)
    if css is None:
        skipped.append("tokens: global.css absent")
    elif not tokens:
        skipped.append("tokens: Stitch theme carries no usable colors")
    else:
        start = css.find(":root {")
        end = css.find("}", start) if start >= 0 else -1
        if start < 0 or end < 0:
            skipped.append("tokens: no :root block in global.css")
        else:
            merged: dict[str, str] = {}
            for line in css[start:end].splitlines()[1:]:
                match = re.match(r"\s*(--[\w-]+)\s*:\s*(.+?)\s*;?\s*$",
                                 line)
                if match:
                    merged[match.group(1)] = match.group(2)
            merged.update(tokens)
            overrides = "\n".join(f"  {k}: {v};"
                                  for k, v in sorted(merged.items()))
            block = (":root {\n" + overrides +
                     "\n  /* Stitch screen theme merged over the base "
                     "tokens; unthemed variables preserved. */")
            css = css[:start] + block + css[end:]
            try:
                with open(os.path.join(site_root, css_rel), "w") as f:
                    f.write(css)
                applied.append(
                    f"tokens: {', '.join(sorted(tokens))}")
            except OSError as e:
                skipped.append(f"tokens: write failed ({e})")

    index = _read(site_root, INDEX_REL)
    if index is None:
        skipped.append("anchor: index.astro absent")
    elif 'id="top"' in index:
        pass
    elif '<section class="hero"' in index:
        index = index.replace('<section class="hero"',
                              '<section id="top" class="hero"', 1)
        try:
            with open(os.path.join(site_root, INDEX_REL), "w") as f:
                f.write(index)
            applied.append("anchor: #top target on hero section")
        except OSError as e:
            skipped.append(f"anchor: write failed ({e})")
    else:
        skipped.append("anchor: no hero section in index.astro")
    return {"applied": applied, "skipped": skipped}


# --- Faithful Stitch mode --------------------------------------------------
#
# Token merging keeps the Astro look with Stitch colors. Faithful mode
# goes further: the generated index page IS the Stitch screen markup
# (sanitized), so the site looks like the Stitch design instead of the
# template wearing its palette. Structure:
#
# - Head: Stitch meta/title/fonts/styles plus the Tailwind CDN loader
#   and its theme config (the classes are meaningless without it).
#   Everything else (scripts, beacons, preloads of remote origins) is
#   dropped. The Tailwind CDN is runtime JavaScript from a third party:
#   kept deliberately for fidelity and flagged in the report + perf
#   audit, which stays advisory.
# - Body: Stitch markup with event-handler attributes, javascript:
#   URLs, remote forms, and embedded scripts removed.
# - Copy honesty: Stitch text routinely invents metrics, certifications,
#   and URLs. Faithful mode ships them anyway (that is the request) but
#   records an explicit human-review warning in the report; body copy is
#   never silently presented as verified.

_ALLOWED_HEAD = {"meta", "title", "link", "style", "script", "base"}
_DROP_BODY = {"script", "iframe", "object", "embed", "form"}


def _keep_head_tag(tag: str, attrs: dict) -> bool:
    if tag == "script":
        src = attrs.get("src", "")
        if src:
            return url_host(src) in TAILWIND_CDN_HOSTS
        return attrs.get("id") == "tailwind-config"
    if tag == "link":
        rel = attrs.get("rel", "")
        href = attrs.get("href", "")
        if rel in ("preconnect", "dns-prefetch"):
            return True
        return bool(href) and url_host(href) in FONT_HOSTS
    return tag in _ALLOWED_HEAD


def _clean_attrs(tag: str, attrs: list[tuple[str, str | None]]) -> dict:
    """Drop event handlers, javascript: URLs, and remote form targets."""
    kept: dict[str, str] = {}
    for name, value in attrs:
        low = name.lower()
        if low.startswith("on"):
            continue
        if low in ("href", "src", "action") and isinstance(value, str):
            if value.strip().lower().startswith("javascript:"):
                continue
        if low == "action" and isinstance(value, str) and \
                value.startswith("http"):
            continue
        if value is None:
            kept[name] = ""
        else:
            kept[name] = value
    return kept


class _Sanitizer:
    """Rebuilds an HTML fragment keeping structure, dropping risky
    tags, and cleaning attributes. Head and body use different
    allowlists."""

    def __init__(self, in_head: bool):
        from html.parser import HTMLParser

        class Rewriter(HTMLParser):
            def __init__(self, outer):
                super().__init__(convert_charrefs=False)
                self.outer = outer
                self.parts: list[str] = []
                self.skip_depth = 0

            def _attrs(self, attrs):
                cleaned = _clean_attrs("", attrs)
                return "".join(
                    f' {k}="{v}"' if v != "" else f" {k}"
                    for k, v in cleaned.items())

            def handle_starttag(self, tag, attrs):
                tag = tag.lower()
                if self.skip_depth:
                    self.skip_depth += 1
                    return
                ad = dict(attrs)
                if self.outer.in_head:
                    if tag not in _ALLOWED_HEAD or \
                            not _keep_head_tag(tag, ad):
                        if tag in ("script", "style"):
                            self.skip_depth = 1
                        return
                elif tag in _DROP_BODY:
                    self.skip_depth = 1
                    return
                self.parts.append(f"<{tag}{self._attrs(attrs)}>")

            def handle_endtag(self, tag):
                tag = tag.lower()
                if self.skip_depth:
                    self.skip_depth -= 1
                    return
                self.parts.append(f"</{tag}>")

            def handle_startendtag(self, tag, attrs):
                tag = tag.lower()
                if self.skip_depth:
                    return
                ad = dict(attrs)
                if self.outer.in_head:
                    if tag not in _ALLOWED_HEAD or \
                            not _keep_head_tag(tag, ad):
                        return
                elif tag in _DROP_BODY:
                    return
                self.parts.append(f"<{tag}{self._attrs(attrs)} />")

            def handle_data(self, data):
                if not self.skip_depth:
                    self.parts.append(data)

            def handle_comment(self, data):
                return

            def handle_decl(self, decl):
                return

        self.rewriter = Rewriter(self)
        self.in_head = in_head

    def run(self, fragment: str) -> str:
        try:
            self.rewriter.feed(fragment[:1 << 20])
        except Exception:
            pass
        return "".join(self.rewriter.parts)


def sanitize_faithful(html: str) -> dict:
    """Split a Stitch document into sanitized head/body fragments.

    Returns {"head": ..., "body": ..., "dropped": [...]} where dropped
    names every removed risky element for the report.
    """
    doc = html or ""
    lower = doc.lower()
    html_attrs = ""
    html_match = re.search(r"<html([^>]*)>", doc, re.I)
    if html_match:
        html_attrs = _clean_attrs("html", re.findall(
            r"([\w-]+)(?:=\"([^\"]*)\")?", html_match.group(1)))
        html_attrs = "".join(
            f' {k}="{v}"' if v != "" else f" {k}"
            for k, v in html_attrs.items() if k != "lang")
    head_inner, body_inner = "", ""
    head_match = re.search(r"<head[^>]*>(.*)</head>", doc,
                           re.S | re.I)
    if head_match:
        head_inner = head_match.group(1)
    body_match = re.search(r"<body([^>]*)>(.*)</body>", doc,
                           re.S | re.I)
    body_attrs, body_inner = "", ""
    if body_match:
        body_attrs = body_match.group(1)
        body_inner = body_match.group(2)
    dropped: list[str] = []
    for pattern, name in (
            (r"<script(?![^>]*(cdn\.tailwindcss\.com|id=\"tailwind-config\"))",
             "non-tailwind script"),
            (r"<iframe", "iframe"),
            (r"\son\w+\s*=", "event-handler attribute"),
            (r"javascript:", "javascript: URL")):
        if re.search(pattern, lower):
            dropped.append(name)
    head = _Sanitizer(in_head=True).run(head_inner)
    body = _Sanitizer(in_head=False).run(body_inner)
    body_attrs_clean = _clean_attrs("body", re.findall(
        r"([\w-]+)(?:=\"([^\"]*)\")?", body_attrs))
    attr_text = "".join(
        f' {k}="{v}"' if v != "" else f" {k}"
        for k, v in body_attrs_clean.items())
    return {"head": head, "body": body, "attrs": attr_text,
            "html_attrs": html_attrs,
            "dropped": sorted(set(dropped))}


def build_faithful_page(head: str, body: str, body_attrs: str,
                        project_name: str, html_attrs: str = "") -> str:
    """Compose the standalone index.astro from sanitized fragments."""
    # Astro bundles <script>/<style> by default, which would eat the
    # Tailwind loader and rewrite Stitch's stylesheet. is:inline keeps
    # both byte-identical so the page renders as Stitch designed it.
    head = re.sub(r"<script(?=[\s>])", "<script is:inline", head)
    head = re.sub(r"<style(?=[\s>])", "<style is:inline", head)
    return (
        "<!DOCTYPE html>\n"
        f"<!-- Generated from the Stitch screen for {project_name}: "
        "body copy is Stitch-generated and MUST be human-reviewed "
        "before publishing (it may invent metrics, certifications, "
        "or URLs). -->\n"
        f'<html lang="en"{html_attrs}>\n<head>\n' + head +
        "\n</head>\n<body" + body_attrs + ">\n" + body +
        "\n</body>\n</html>\n"
    )


def apply_faithful_page(site_root: str, html: str,
                        project_name: str) -> dict:
    """Replace the generated index with the sanitized Stitch document.

    Preserves nothing of the template page (that is the point); the
    FAQ-placeholder contract is therefore released by the caller via
    the faithful flag. Returns {"applied": [...], "skipped": [...]}.
    """
    parts = sanitize_faithful(html)
    if not parts["body"].strip():
        return {"applied": [],
                "skipped": ["faithful: Stitch body empty after sanitize"]}
    page = build_faithful_page(parts["head"], parts["body"],
                               parts["attrs"], project_name,
                               parts.get("html_attrs", ""))
    if page.count("<h1") > 1:
        page = _demote_extra_h1(page)
    try:
        with open(os.path.join(site_root, INDEX_REL), "w") as f:
            f.write(page)
    except OSError as e:
        return {"applied": [], "skipped": [f"faithful: write failed ({e})"]}
    applied = ["faithful: index is the Stitch screen markup (sanitized)"]
    if parts["dropped"]:
        applied.append(f"faithful: dropped {', '.join(parts['dropped'])}")
    return {"applied": applied, "skipped": []}


def _demote_extra_h1(page: str) -> str:
    """Keep the first h1, demote the rest to h2 so the single-h1 gate
    holds on Stitch documents with several top headings."""
    parts = re.split(r"(<h1[\s>]|</h1>)", page)
    out: list[str] = []
    seen_first_open = False
    open_stack: list[bool] = []
    for part in parts:
        if re.fullmatch(r"<h1[\s>]", part):
            if not seen_first_open:
                seen_first_open = True
                open_stack.append(True)
                out.append(part)
            else:
                open_stack.append(False)
                out.append("<h2" + part[3:])
        elif part == "</h1>":
            first = open_stack.pop() if open_stack else True
            out.append("</h1>" if first else "</h2>")
        else:
            out.append(part)
    return "".join(out)


# --- Static Tailwind build ---------------------------------------------------
#
# Faithful mode must not depend on the Tailwind Play CDN at view time:
# offline viewers (or a slow/blocked CDN) see an unstyled page, which
# reads as "way worse than Stitch". The journey therefore compiles the
# Stitch theme to static CSS with the Tailwind CLI and self-hosts the
# fonts, keeping the CDN path only as a fallback. Everything below is
# pure and offline-testable; the npm/network half lives in
# design_apply_stage with an injectable runner.

TAILWIND_CSS_INPUT = "@tailwind base;\n@tailwind components;\n@tailwind utilities;\n"
TAILWIND_VERSION = "3"
TAILWIND_PLUGIN_PACKAGES = {
    "forms": "@tailwindcss/forms",
    "typography": "@tailwindcss/typography",
    "container-queries": "@tailwindcss/container-queries",
    "line-clamp": "@tailwindcss/line-clamp",
}


def extract_tailwind_config(html: str) -> dict:
    """Pull the Tailwind theme out of a Stitch document.

    Returns {"config_js": <module.exports = {...} source or "">,
    "plugins": [cdn plugin names], "present": bool}. The config object
    is Stitch's own theme (darkMode class, full color system); it is
    reused verbatim so the static build matches the CDN render.
    """
    plugins: list[str] = []
    cdn = re.search(
        r'https://cdn\.tailwindcss\.com(\?[^"\']*)?', html or "")
    if cdn and cdn.group(1):
        query = cdn.group(1)
        match = re.search(r"[?&]plugins=([^&\"']*)", query)
        if match:
            plugins = [p for p in match.group(1).split(",") if p]
    config_js = ""
    cfg = re.search(
        r'<script[^>]*id="tailwind-config"[^>]*>(.*?)</script>',
        html or "", re.S | re.I)
    if cfg:
        body = cfg.group(1).strip()
        body = re.sub(r"^\s*tailwind\.config\s*=\s*", "", body)
        body = re.sub(r";\s*$", "", body)
        if body.startswith("{") and body.endswith("}"):
            config_js = "module.exports = " + body + ";\n"
    return {"config_js": config_js, "plugins": plugins,
            "present": bool(config_js)}


def tailwind_npm_packages(plugins: list[str]) -> list[str]:
    """npm packages for a static build: pinned Tailwind v3 plus the
    CDN plugins the document asked for. Unknown plugin names are
    dropped (recorded by the caller), never passed to npm."""
    packages = [f"tailwindcss@{TAILWIND_VERSION}"]
    for name in plugins or []:
        package = TAILWIND_PLUGIN_PACKAGES.get(name)
        if package:
            packages.append(package)
    return packages


def tailwind_config_file(config_js: str, plugins: list[str]) -> str:
    """tailwind.config.js source: Stitch theme plus required plugins
    and content globs covering the Astro tree."""
    requires = ", ".join(f'require("{TAILWIND_PLUGIN_PACKAGES[p]}")'
                         for p in plugins or []
                         if p in TAILWIND_PLUGIN_PACKAGES)
    body = config_js or "module.exports = {};\n"
    body = re.sub(r"module\.exports\s*=\s*\{",
                  "module.exports = {\n  content: "
                  "['./src/**/*.{astro,html,js,ts}'],",
                  body, count=1)
    if requires:
        body = body.rstrip() + f"\nmodule.exports.plugins = [{requires}];\n"
    return body


def rewrite_head_static(head: str) -> str:
    """Drop the Tailwind CDN loader + inline config from a faithful
    head: the static stylesheet (imported via frontmatter) replaces
    them. Fonts stay here; self-hosting rewrites them separately."""
    head = re.sub(r'<script[^>]*cdn\.tailwindcss\.com[^>]*>\s*</script>',
                  "", head)
    head = re.sub(r'<script[^>]*id="tailwind-config"[^>]*>.*?</script>',
                  "", head, flags=re.S)
    return head


def collect_font_css_urls(head: str) -> list[str]:
    """Google-Fonts css2 URLs in document order, deduplicated."""
    seen: list[str] = []
    for match in re.finditer(
            r'<link[^>]+href="([^"]*fonts\.googleapis\.com/css2[^"]*)"',
            head or ""):
        url = match.group(1).replace("&amp;", "&")
        if url not in seen:
            seen.append(url)
    return seen


def rewrite_fonts_local(head: str, local_href: str = "/fonts.css") -> str:
    """Replace Google-Fonts <link> tags (stylesheet + preconnects)
    with one self-hosted stylesheet reference."""
    head = re.sub(r'<link[^>]+fonts\.googleapis\.com[^>]*/?>', "", head)
    head = re.sub(r'<link[^>]+fonts\.gstatic\.com[^>]*/?>', "", head)
    marker = "<!-- self-hosted fonts -->"
    if marker not in head:
        head = head.rstrip() + (
            f"\n{marker}\n"
            f'<link rel="stylesheet" href="{local_href}" />\n')
    return head


def write_tailwind_sources(site_root: str, config_js: str,
                           plugins: list[str]) -> dict:
    """Write tailwind.config.js (Stitch theme + content globs) and the
    input stylesheet for the CLI build. Returns written rel paths."""
    config_src = tailwind_config_file(config_js, plugins)
    written = []
    try:
        with open(os.path.join(site_root, "tailwind.config.js"),
                  "w") as f:
            f.write(config_src)
        written.append("tailwind.config.js")
        with open(os.path.join(site_root, "stitch.input.css"), "w") as f:
            f.write(TAILWIND_CSS_INPUT)
        written.append("stitch.input.css")
    except OSError as e:
        return {"written": written, "error": str(e)}
    return {"written": written, "error": ""}


def finalize_static_page(site_root: str) -> dict:
    """Swap a faithful page from CDN Tailwind to the static build:
    drop loader+config, import ../styles/stitch.css via frontmatter.
    Returns {"applied": [...], "skipped": [...]}."""
    try:
        with open(os.path.join(site_root, INDEX_REL)) as f:
            page = f.read()
    except OSError:
        return {"applied": [], "skipped": ["static: index unreadable"]}
    if "../styles/stitch.css" in page:
        return {"applied": [], "skipped": ["static: already wired"]}
    if not any(url_host(s) in TAILWIND_CDN_HOSTS
              for s in scan_page(page).external_scripts):
        return {"applied": [], "skipped": ["static: no CDN loader found"]}
    head_match = re.search(r"(<head[^>]*>)(.*)(</head>)", page,
                           re.S | re.I)
    if not head_match:
        return {"applied": [], "skipped": ["static: no head element"]}
    new_head = rewrite_head_static(head_match.group(2))
    page = (page[:head_match.start(2)] + new_head +
            page[head_match.end(2):])
    frontmatter = '---\nimport "../styles/stitch.css";\n---\n'
    if page.lstrip().startswith("<!DOCTYPE"):
        page = frontmatter + page
    else:
        return {"applied": [], "skipped": ["static: unexpected page shape"]}
    try:
        with open(os.path.join(site_root, INDEX_REL), "w") as f:
            f.write(page)
    except OSError as e:
        return {"applied": [], "skipped": [f"static: write failed ({e})"]}
    return {"applied": ["static: Tailwind compiled to styles/stitch.css"],
            "skipped": []}


def fetch_font_package(css_urls: list[str],
                       fetcher) -> dict:
    """Download Google-Fonts css2 sheets (woff2 UA) plus every
    referenced woff2 file. fetcher(url) -> bytes; raises on failure.
    Returns {"css": merged css text with /fonts/ URLs,
    "files": {name: bytes}}. Bounded: at most 4 sheets; callers cap
    total bytes."""
    merged: list[str] = []
    files: dict[str, bytes] = {}
    pending: list[tuple[str, str]] = []
    counter = [0]

    def collect(match):
        url = match.group(1)
        counter[0] += 1
        name = f"font-{counter[0]}.woff2"
        pending.append((name, url))
        return f"url(/fonts/{name})"

    for url in css_urls[:4]:
        sheet = fetcher(url).decode("utf-8", "replace")
        sheet = re.sub(r"url\((https://[^)]+\.woff2[^)]*)\)", collect,
                       sheet)
        merged.append(sheet)
    for name, url in pending:
        files[name] = fetcher(url)
    return {"css": "\n".join(merged), "files": files}


def write_font_package(site_root: str, css: str,
                       files: dict[str, bytes]) -> dict:
    """Write public/fonts.css plus public/fonts/*.woff2. Returns
    {"applied": [...], "skipped": [...]}."""
    try:
        fonts_dir = os.path.join(site_root, "public", "fonts")
        os.makedirs(fonts_dir, exist_ok=True)
        for name, data in files.items():
            if not re.fullmatch(r"font-\d+\.woff2", name):
                continue
            with open(os.path.join(fonts_dir, name), "wb") as f:
                f.write(data)
        with open(os.path.join(site_root, "public", "fonts.css"),
                  "w") as f:
            f.write(css)
    except OSError as e:
        return {"applied": [], "skipped": [f"fonts: write failed ({e})"]}
    return {"applied": [f"fonts: self-hosted {len(files)} files"],
            "skipped": []}


# --- Color science (contrast-guaranteed light mode) --------------------------
#
# Stitch ships dark-first themes. A real light mode cannot be a class
# toggle alone: utilities carry dark hexes. The journey rewrites the
# Tailwind config so every color flows through a CSS variable, emits
# :root (light, derived) and .dark (Stitch, verbatim) palettes, and
# verifies every text/surface pair hits WCAG AA (4.5:1) in BOTH modes,
# adjusting automatically. No eyeballing, no broken light theme.

def _hex_to_rgb(value: str) -> tuple[float, float, float]:
    value = value.lstrip("#")
    return tuple(int(value[i:i + 2], 16) / 255.0 for i in (0, 2, 4))


def _rgb_to_hex(rgb: tuple[float, float, float]) -> str:
    return "#{:02x}{:02x}{:02x}".format(
        *[max(0, min(255, round(c * 255))) for c in rgb])


def mix_hex(first: str, second: str, amount: float) -> str:
    """Linear mix of two #rrggbb colors; amount 0 → first, 1 → second."""
    a, b = _hex_to_rgb(first), _hex_to_rgb(second)
    return _rgb_to_hex(tuple(x + (y - x) * amount for x, y in zip(a, b)))


def luminance(value: str) -> float:
    """WCAG relative luminance of a #rrggbb color."""
    def channel(c: float) -> float:
        return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4
    r, g, b = _hex_to_rgb(value)
    return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)


def contrast_ratio(first: str, second: str) -> float:
    """WCAG contrast ratio between two #rrggbb colors (1–21)."""
    lighter = max(luminance(first), luminance(second))
    darker = min(luminance(first), luminance(second))
    return (lighter + 0.05) / (darker + 0.05)


def _ensure_contrast(fg: str, bg: str, minimum: float = 4.5) -> str:
    """Nudge fg toward black or white until it contrasts bg at minimum.

    Direction is decided by what WORKS, not by fg's own luminance: dark
    text on a dark accent surface must go the other way (toward
    white), which a luminance-based pick cannot do. Both directions are
    searched; the successful one closest to the original wins.
    """
    if contrast_ratio(fg, bg) >= minimum:
        return fg

    def search(target: str) -> str:
        lo, hi, best = 0.0, 1.0, ""
        for _ in range(25):
            mid = (lo + hi) / 2
            candidate = mix_hex(fg, target, mid)
            if contrast_ratio(candidate, bg) >= minimum:
                best, hi = candidate, mid
            else:
                lo = mid
        return best

    darker, lighter = search("#000000"), search("#ffffff")
    options = [c for c in (darker, lighter) if c]
    if not options:
        return fg
    return min(options, key=lambda c: abs(luminance(c) - luminance(fg)))


def _bare_token(name: str) -> str:
    """Palette variable names carry a --c- prefix; classification and
    pairing work on the bare Stitch token path."""
    lowered = name.lower()
    if lowered.startswith("--c-"):
        return lowered[len("--c-"):]
    return lowered


def _is_text_token(name: str) -> bool:
    lowered = _bare_token(name)
    # Stitch uses hyphens ("on-surface"); normalize so token
    # classification works for both spellings.
    flattened = lowered.replace("-", "_")
    return flattened.startswith("on_") or flattened.startswith("inverse_")


def _is_surface_name(name: str) -> bool:
    """True for tokens that carry a background in Stitch's palette."""
    flattened = _bare_token(name).replace("-", "_")
    return ("surface" in flattened or "container" in flattened
            or "fixed" in flattened or flattened == "background")


def derive_light_value(name: str, dark_hex: str) -> str:
    """Map one dark-theme color to its light-mode counterpart.

    Light mode must stay legible on near-white paper regardless of
    which token Stitch used it for, so the rules are contrast-first:

    * surface tokens (background/surface/…-container) go near-white,
      hue-tinted, so panels still read as panels;
    * text tokens (on-*, inverse) go near-black;
    * every remaining token (accents, outlines, primaries) is darkened
      until it clears 4.5:1 against white — a vivid dark-theme cyan is
      invisible on paper otherwise.

    The pair pass in build_palettes verifies the result; this function
    only gets close enough for it to succeed.
    """
    if not re.fullmatch(r"#[0-9a-fA-F]{6}", dark_hex or ""):
        return dark_hex
    if _is_text_token(name):
        return mix_hex(dark_hex, "#0b0d12", 0.72)
    if _is_surface_name(name):
        return mix_hex(dark_hex, "#ffffff", 0.94)
    if contrast_ratio(dark_hex, LIGHT_PAPER) >= 4.5:
        return dark_hex
    return _ensure_contrast(mix_hex(dark_hex, "#000000", 0.35), LIGHT_PAPER)


_PAIR_BASES = ("background", "surface", "surface_container",
               "surface_container_low", "primary", "primary_container",
               "secondary", "secondary_container", "tertiary",
               "tertiary_container", "error", "error_container")

# Light-mode paper: surfaces are mixed toward white, so accents are
# checked against a slightly darker paper than the final mix, which
# leaves headroom (a color that just clears #eeeeee also clears the
# #f1f1f1 surface it actually sits on).
LIGHT_PAPER = "#eeeeee"


def _find(dark: dict[str, str], bare: str) -> str:
    """Palette key for a bare token path (prefix-aware, separator-
    tolerant: Stitch uses hyphens, callers may say on_background)."""
    if bare in dark:
        return bare
    for candidate in (f"--c-{bare}", f"--c-{bare.replace('_', '-')}",
                      f"--c-{bare.replace('-', '_')}"):
        if candidate in dark:
            return candidate
    return ""


def _pair_candidates(dark: dict[str, str]) -> list[tuple[str, str]]:
    """(surface, text) token pairs present in the theme, with
    fallbacks so background/on-background always verify."""
    pairs = []
    for base in _PAIR_BASES:
        surface, text = _find(dark, base), _find(dark, "on_" + base)
        if surface and text:
            pairs.append((surface, text))
    if not any(s == _find(dark, "surface") for s, _ in pairs):
        surface = _find(dark, "surface") or _find(dark, "background")
        text = (_find(dark, "on_surface") or _find(dark, "on_background"))
        if surface and text:
            pairs.append((surface, text))
    if not pairs:
        surface = _find(dark, "background")
        text = _find(dark, "on_background")
        if surface and text:
            pairs.append((surface, text))
    return pairs


def build_palettes(dark: dict[str, str]) -> dict:
    """Derive the light palette and verify every pair in both modes.

    Returns {"dark": {...}, "light": {...}, "pairs": [...],
    "contrast": {(mode, surface, text): ratio}}. Ratios below 4.5 are
    adjusted on the light side until they pass; dark ratios are
    reported verbatim (Stitch's own design, never rewritten).
    """
    light = {name: derive_light_value(name, value)
             for name, value in dark.items()}
    # Any text/accent token must also stand out on the derived light
    # page background, not just on its own pairs: Stitch uses accents
    # as link and badge text on top of the page surface. Surfaces are
    # skipped — adjusting them against themselves is meaningless.
    page_bg = light.get(_find(light, "background"), LIGHT_PAPER)
    for name, value in list(light.items()):
        if not _is_text_token(name) and _is_surface_name(name):
            continue
        if contrast_ratio(value, page_bg) < 4.5:
            light[name] = _ensure_contrast(value, page_bg)
    contrast: dict[tuple[str, str, str], float] = {}
    for surface, text in _pair_candidates(dark):
        contrast[("dark", surface, text)] = round(
            contrast_ratio(dark[surface], dark[text]), 2)
        bg, fg = light[surface], light[text]
        fg = _ensure_contrast(fg, bg)
        if contrast_ratio(bg, fg) < 4.5:
            bg = mix_hex(bg, "#ffffff", 0.5)
            fg = _ensure_contrast(fg, bg)
        light[surface], light[text] = bg, fg
        contrast[("light", surface, text)] = round(
            contrast_ratio(bg, fg), 2)
    return {"dark": dark, "light": light,
            "pairs": _pair_candidates(dark), "contrast": contrast}


def var_name(path: str) -> str:
    """CSS variable for a config color path: --c-<segments>."""
    slug = re.sub(r"[^a-z0-9]+", "-", path.lower()).strip("-")
    return f"--c-{slug}"


def var_indirect_config(config_js: str) -> tuple[str, dict[str, str]]:
    """Rewrite a Tailwind config so colors flow through CSS variables.

    Every #rrggbb color value becomes rgb(var(--c-path) /
    <alpha-value>), preserving opacity modifiers. Returns the new
    config source plus {var: hex} (the dark palette). A small scanner
    tracks object nesting so duplicate leaf names stay unique.
    """
    start = config_js.find("colors")
    if start < 0:
        return config_js, {}
    i = config_js.find("{", start)
    if i < 0:
        return config_js, {}
    depth = 0
    end = -1
    for j in range(i, len(config_js)):
        if config_js[j] == "{":
            depth += 1
        elif config_js[j] == "}":
            depth -= 1
            if depth == 0:
                end = j
                break
    if end < 0:
        return config_js, {}
    head, body, tail = config_js[:i], config_js[i:end + 1], config_js[end + 1:]
    palette: dict[str, str] = {}
    out: list[str] = []
    # Scanner over the colors object: pending key, key stack, and a
    # cursor. Strings are consumed whole so braces inside them (none
    # expected, but safe) cannot corrupt depth tracking.
    stack: list[str] = []
    pending: str | None = None
    pos = 0
    n = len(body)

    def read_string(at: int) -> tuple[str, int]:
        assert body[at] == '"'
        k = at + 1
        while k < n and body[k] != '"':
            k += 1
        return body[at + 1:k], k + 1

    while pos < n:
        char = body[pos]
        if char == '"':
            text, after = read_string(pos)
            look = after
            while look < n and body[look] in " \t\r\n":
                look += 1
            if look < n and body[look] == ":":
                pending = text
                out.append(body[pos:after])
                pos = after
                continue
            if pending is not None and re.fullmatch(
                    r"#[0-9a-fA-F]{6}", text):
                path = "-".join(stack + [pending])
                var = var_name(path)
                palette[var] = text
                out.append(f'"rgb(var({var}) / <alpha-value>)"')
                pending = None
                pos = after
                continue
            out.append(body[pos:after])
            pos = after
            continue
        if char == "{":
            if pending is not None:
                stack.append(pending)
                pending = None
            out.append(char)
            pos += 1
            continue
        if char == "}":
            if stack:
                stack.pop()
            pending = None
            out.append(char)
            pos += 1
            continue
        if char == ":":
            out.append(char)
            pos += 1
            continue
        out.append(char)
        pos += 1
    return head + "".join(out) + tail, palette


def light_mode_safety_css(palette_keys: list[str]) -> str:
    """Light-mode safety net for colored buttons.

    Stitch buttons paint a token background (`bg-primary-container`,
    `bg-accent`, …) with plain white text. In light mode those token
    backgrounds are near-white, so white text vanishes. Tailwind emits
    a class per config color, so the fix is deterministic: force the
    page's ink on light backgrounds.
    """
    backgrounds = []
    for key in palette_keys:
        bare = _bare_token(key)
        flat = bare.replace("-", "_")
        if _is_text_token(bare) or flat == "background":
            continue
        if ("primary" in flat or "secondary" in flat or "tertiary" in flat
                or "container" in flat or "accent" in flat
                or "error" in flat):
            backgrounds.append(".bg-" + bare)
    if not backgrounds:
        return ""
    rules = ", ".join(f"html:not(.dark) {cls}" for cls in backgrounds)
    return (f"\n{rules} {{\n"
            f"  color: var(--c-background) !important;\n}}\n"
            f"{rules} svg, {rules} .material-symbols-outlined {{\n"
            f"  color: inherit !important;\n}}\n")


def palette_stylesheet(palettes: dict) -> str:
    """`:root` (light) + `.dark` (Stitch) variable definitions."""
    def block(selector: str, values: dict) -> str:
        lines = [f"{selector} {{", "  color-scheme: "
                 + ("dark" if selector != ":root" else "light") + ";"]
        for var in sorted(values):
            r, g, b = _hex_to_rgb(values[var])
            lines.append(f"  {var}: {int(r * 255)} {int(g * 255)} "
                         f"{int(b * 255)};")
        lines.append("}")
        return "\n".join(lines)

    dark = palettes.get("dark", {})
    light = palettes.get("light", {})
    return (block(":root", light) + "\n" +
            block("html.dark", dark) + "\n")


THEME_INIT_SCRIPT = """(function(){try{var s='stitch-theme';var t=null;try{t=localStorage.getItem(s)}catch(e){}if(!t){try{t=matchMedia('(prefers-color-scheme: light)').matches?'light':'dark'}catch(e){t='dark'}}var h=document.documentElement;if(t==='light'){h.classList.remove('dark')}else{h.classList.add('dark')}function paint(btn){if(!btn)return;var light=!h.classList.contains('dark');btn.setAttribute('aria-pressed',String(!light));btn.setAttribute('aria-label',light?'Switch to dark mode':'Switch to light mode');var lab=btn.querySelector('[data-theme-label]');if(lab){lab.textContent=light?'Dark':'Light'}}function bind(){var btn=document.getElementById('theme-toggle');if(!btn)return;paint(btn);btn.addEventListener('click',function(){try{var light=h.classList.contains('dark');if(light){h.classList.remove('dark');try{localStorage.setItem(s,'light')}catch(e){}}else{h.classList.add('dark');try{localStorage.setItem(s,'dark')}catch(e){}}paint(btn)}catch(e){}})}if(document.readyState==='loading'){document.addEventListener('DOMContentLoaded',bind)}else{bind()}}catch(e){}})();"""

THEME_TOGGLE_STYLE = """
.theme-toggle{position:fixed;right:1rem;bottom:1rem;z-index:50;display:inline-flex;align-items:center;gap:.5rem;padding:.55rem .9rem;border-radius:999px;border:1px solid var(--line,#e0e0e0);background:var(--surface,#f6f6f6);color:var(--ink,#1a1a1a);font:600 .8rem/1 system-ui,sans-serif;cursor:pointer;box-shadow:0 4px 16px rgba(0,0,0,.18)}
.theme-toggle:focus-visible{outline:3px solid var(--accent,#0b5fff);outline-offset:2px}
@media (prefers-reduced-motion: reduce){.theme-toggle{transition:none}}
"""

THEME_TOGGLE_BUTTON = """
<button id="theme-toggle" class="theme-toggle" type="button" aria-pressed="false" aria-label="Switch to light mode"><span aria-hidden="true">◐</span><span data-theme-label>Light</span></button>
"""


# --- Production SEO / GEO / AEO ----------------------------------------------
#
# A Stitch prototype has none of this: no description, no canonical,
# no social cards, no structured data, no sitemap, no crawler policy
# beyond Allow, no answer-engine surface. The emitters below write the
# full set from the brief (+ --domain when given); the auditors verify
# it. Everything is deterministic: no dates (sitemap omits lastmod),
# no invented URLs (absolute references exist only with a domain).

STOPWORDS = frozenset(
    "the and for with from that this into your you our are was were "
    "will with without about across between through during before after "
    "they them their then than also such only over under who what when "
    "where which while your yours ours their theirs his her its our our "
    "and or the a an of to in on at by as is it be we us he she they "
    "not no nor but if then else when while".split())


def significant_terms(text: str) -> list[str]:
    """Lowercase words ≥5 letters minus stopwords, order preserved,
    deduplicated. The ICP vocabulary of a brief."""
    seen: list[str] = []
    for word in re.findall(r"[a-zA-Z]{5,}", (text or "").lower()):
        if word not in STOPWORDS and word not in seen:
            seen.append(word)
    return seen


def meta_description(brief: dict, limit: int = 155) -> str:
    """One description from purpose + success metric, hard-capped for
    search-result display. Never invented: only brief words."""
    text = " ".join(str(brief.get(key, "")) for key in
                    ("purpose", "success_metric")).strip()
    text = re.sub(r"\s+", " ", text)
    if len(text) <= limit:
        return text
    cut = text[:limit].rsplit(" ", 1)[0]
    return cut[:limit - 1] + "…"


def absolute_url(domain: str, path: str) -> str:
    """https URL for a site path; empty without a domain (callers skip
    absolute references then, never invent a host)."""
    domain = (domain or "").strip().rstrip("/")
    if not domain or "://" in domain or " " in domain:
        return ""
    if not path.startswith("/"):
        path = "/" + path
    return f"https://{domain}{path}"


def seo_head_extras(brief: dict, domain: str = "",
                    faithful: bool = False,
                    has_faq: bool = False) -> dict:
    """Head fragments + files for production search/social presence.

    Returns {"tags": [...html lines...], "jsonld": [dicts],
    "skipped": [...]}. Absolute URLs (canonical, OG, sitemap) exist
    only with --domain; without it the page stays relative and honest.
    """
    tags: list[str] = []
    skipped: list[str] = []
    name = str(brief.get("project_name", "site"))
    description = meta_description(brief)
    if description:
        tags.append(f'<meta name="description" content="{description}" />')
    canonical = absolute_url(domain, "/")
    if canonical:
        tags.append(f'<link rel="canonical" href="{canonical}" />')
    else:
        skipped.append("seo: no canonical without --domain")
    tags.append('<meta property="og:type" content="website" />')
    tags.append(f'<meta property="og:title" content="{name}" />')
    if description:
        tags.append(f'<meta property="og:description" content="{description}" />')
    if canonical:
        tags.append(f'<meta property="og:url" content="{canonical}" />')
    image = absolute_url(domain, "/og.svg")
    if image:
        tags.append(f'<meta property="og:image" content="{image}" />')
        tags.append('<meta name="twitter:card" '
                    'content="summary_large_image" />')
    else:
        tags.append('<meta name="twitter:card" content="summary" />')
        skipped.append("seo: no og:image without --domain")
    tags.append(f'<meta name="twitter:title" content="{name}" />')
    if description:
        tags.append('<meta name="twitter:description" '
                    f'content="{description}" />')
    tags.append('<meta name="color-scheme" content="dark light" />')
    scope = str(brief.get("geographic_scope", ""))
    jsonld: list[dict] = [
        {"@context": "https://schema.org", "@type": "WebSite",
         "name": name, **({"url": canonical} if canonical else {}),
         "description": description,
         "inLanguage": "en"},
        {"@context": "https://schema.org", "@type": "Organization",
         "name": name, "description": description},
    ]
    if not faithful and has_faq:
        jsonld.append({"@context": "https://schema.org",
                       "@type": "FAQPage", "mainEntity": []})
    return {"tags": tags, "jsonld": jsonld, "skipped": skipped,
            "description": description, "canonical": canonical,
            "scope": scope}


def write_seo_files(site_root: str, brief: dict,
                    domain: str = "") -> dict:
    """Write robots.txt, sitemap.xml, og.svg, _headers, and refresh
    llms.txt. Deterministic (no dates). Returns applied/skipped."""
    applied: list[str] = []
    skipped: list[str] = []
    name = str(brief.get("project_name", "site"))
    try:
        sitemap = absolute_url(domain, "/sitemap.xml")
        robots = "User-agent: *\nAllow: /\n"
        if sitemap:
            robots += f"Sitemap: {sitemap}\n"
        else:
            skipped.append("seo: sitemap reference needs --domain")
        with open(os.path.join(site_root, "public", "robots.txt"),
                  "w") as f:
            f.write(robots)
        applied.append("seo: robots.txt")
        url = absolute_url(domain, "/")
        if url:
            with open(os.path.join(site_root, "public", "sitemap.xml"),
                      "w") as f:
                f.write('<?xml version="1.0" encoding="UTF-8"?>\n'
                        '<urlset xmlns="http://www.sitemaps.org/schemas/'
                        'sitemap/0.9">\n'
                        f'  <url><loc>{url}</loc></url>\n'
                        "</urlset>\n")
            applied.append("seo: sitemap.xml")
        else:
            skipped.append("seo: sitemap.xml needs --domain")
    except OSError as e:
        skipped.append(f"seo: crawler files failed ({e})")
    try:
        svg = (f'<svg xmlns="http://www.w3.org/2000/svg" width="1200" '
               f'height="630"><rect width="1200" height="630" '
               f'fill="#101419"/><text x="80" y="300" font-family="'
               f'sans-serif" font-size="72" font-weight="700" '
               f'fill="#ffffff">{name}</text><text x="80" y="380" '
               f'font-family="sans-serif" font-size="32" '
               f'fill="#9aa4b2">{meta_description(brief, 80)}</text>'
               f"</svg>\n")
        with open(os.path.join(site_root, "public", "og.svg"), "w") as f:
            f.write(svg)
        applied.append("seo: og.svg social image")
    except OSError as e:
        skipped.append(f"seo: og.svg failed ({e})")
    return {"applied": applied, "skipped": skipped}


def csp_for_page(page: str) -> str:
    """Content-Security-Policy from what the page actually contains:
    hashes for every inline script/style block, self for the rest.
    Falls back to 'unsafe-inline' only when hashing is impossible
    (recorded by the caller via a sentinel check)."""
    import base64
    import hashlib

    scanner = scan_page(page)
    directives = ["default-src 'self'",
                  "base-uri 'self'",
                  "frame-ancestors 'none'",
                  "form-action 'self'",
                  "img-src 'self' data:",
                  "font-src 'self' data:",
                  "connect-src 'self'"]

    def hashes(bodies: list[str]) -> str:
        out = []
        for body in bodies:
            if body.strip():
                digest = hashlib.sha256(body.encode()).digest()
                out.append("'sha256-" +
                           base64.b64encode(digest).decode() + "'")
        return " ".join(out)

    script_hashes = hashes(scanner.inline_scripts)
    directives.append("script-src 'self'" +
                      (f" {script_hashes}" if script_hashes else ""))
    style_hashes = hashes(scanner.inline_styles)
    directives.append("style-src 'self'" +
                      (f" {style_hashes}" if style_hashes else ""))
    return "; ".join(directives)


def write_security_headers(site_root: str, page: str) -> dict:
    """Write public/_headers (Netlify + Cloudflare Pages convention):
    hash-pinned CSP plus hardening headers. Returns applied/skipped."""
    csp = csp_for_page(page)
    body = ("/*\n"
            f"  Content-Security-Policy: {csp}\n"
            "  X-Frame-Options: DENY\n"
            "  X-Content-Type-Options: nosniff\n"
            "  Referrer-Policy: strict-origin-when-cross-origin\n"
            "  Permissions-Policy: camera=(), microphone=(), "
            "geolocation=()\n"
            "  Strict-Transport-Security: max-age=63072000; "
            "includeSubDomains\n")
    try:
        with open(os.path.join(site_root, "public", "_headers"),
                  "w") as f:
            f.write(body)
    except OSError as e:
        return {"applied": [], "skipped": [f"security: _headers ({e})"]}
    return {"applied": ["security: _headers with hash-pinned CSP"],
            "skipped": []}


# --- ICP filtering ------------------------------------------------------------
#
# "Perfect ICP filtering" means the page selects its reader: ideal
# customers see themselves, everyone else self-selects out, and nothing
# promises what the brief forbids. Lexical and deterministic: required
# terms come from the brief's ICP/audience/purpose/metric, forbidden
# patterns from universal fabrication shapes (money, metrics,
# credentials, fake destinations). Scores advise; faithful Stitch copy
# is expected to trip forbidden hits (recorded for human review).

FORBIDDEN_PATTERNS = (
    (r"\$\s?\d", "money amount"),
    (r"\d+\s*%", "percentage claim"),
    (r"\b\d+(\.\d+)?\s?x\b", "multiplier claim"),
    (r"\b\d{2,}(,\d{3})*\+?\b(?!\s*(px|rem|em|ms|s\b|kb|mb))", "bare statistic"),
    (r"https?://(?!.*(w3\.org|schema\.org|sitemaps\.org))",
     "absolute URL"),
)


def _page_text(page: str) -> str:
    return page_text(page)


def icp_filter_report(page: str, brief: dict | None = None) -> dict:
    """Score page copy against the brief's ICP. Returns {"score":
    0–100, "covered": [...], "missing": [...], "forbidden": [...]}."""
    brief = brief or {}
    text = _page_text(page).lower()
    vocab = significant_terms(" ".join(str(brief.get(key, "")) for key in
                                       ("ideal_customer_profile",
                                        "target_audience", "purpose",
                                        "success_metric")))
    covered = [term for term in vocab if term in text]
    missing = [term for term in vocab if term not in text]
    score = round(100 * len(covered) / len(vocab)) if vocab else 100
    forbidden: list[str] = []
    for pattern, label in FORBIDDEN_PATTERNS:
        if re.search(pattern, _page_text(page)):
            forbidden.append(label)
    return {"score": score, "covered": covered[:12],
            "missing": missing[:12], "forbidden": forbidden}


# --- Production auditors (extend the review mesh) ------------------------------

def audit_seo_full(root: str, brief: dict | None = None,
                   domain: str = "") -> dict:
    """Full search/social audit over the built tree: head tags, social
    cards, crawler files, structured data, titles. Advisory."""
    issues: list[str] = []
    checked = 0
    page = _index_source(root)
    if not page:
        return {"ok": False, "issues": ["seo: index unreadable"],
                "checked": checked}
    head_match = re.search(r"<head[^>]*>(.*)</head>", page, re.S | re.I)
    head = head_match.group(1) if head_match else ""
    checked += 1
    title = re.search(r"<title>(.*?)</title>", head, re.S | re.I)
    if not title or not title.group(1).strip():
        issues.append("seo: missing <title>")
    elif not 20 <= len(title.group(1).strip()) <= 65:
        issues.append("seo: title length outside 20–65 chars")
    checked += 1
    desc = re.search(
        r'<meta[^>]+name="description"[^>]+content="([^"]*)"',
        head, re.I)
    if not desc or not desc.group(1).strip():
        issues.append("seo: missing meta description")
    elif not 70 <= len(desc.group(1)) <= 170:
        issues.append("seo: meta description outside 70–170 chars")
    checked += 1
    for prop in ("og:title", "og:type", "og:description"):
        if prop not in head:
            issues.append(f"seo: missing {prop}")
            break
    else:
        checked += 1
    checked += 1
    if "twitter:card" not in head:
        issues.append("seo: missing twitter card")
    checked += 1
    if domain and "rel=\"canonical\"" not in head:
        issues.append("seo: missing canonical with --domain set")
    if not domain and ("og:image" in head or "rel=\"canonical\"" in head):
        issues.append("seo: absolute URL without --domain (invented host?)")
    for rel in ("public/robots.txt",):
        checked += 1
        if _read(root, rel) is None:
            issues.append(f"seo: missing {rel}")
    checked += 1
    if domain and _read(root, "public/sitemap.xml") is None:
        issues.append("seo: missing sitemap.xml with --domain set")
    checked += 1
    if "application/ld+json" not in page:
        issues.append("seo: no JSON-LD structured data")
    return {"ok": not issues, "issues": issues, "checked": checked}


def audit_aeo(root: str, brief: dict | None = None) -> dict:
    """Answer-engine audit: direct answers, question headings, FAQ or
    HowTo schema, llms.txt. Advisory."""
    issues: list[str] = []
    checked = 0
    page = _index_source(root)
    if not page:
        return {"ok": False, "issues": ["aeo: index unreadable"],
                "checked": checked}
    text = _page_text(page)
    checked += 1
    questions = re.findall(r"<h[23][^>]*>([^<]*\?[^<]*)</h[23]>", page)
    if not questions and "FAQPage" not in page and "HowTo" not in page:
        issues.append("aeo: no question headings or Q&A schema")
    checked += 1
    if "FAQPage" in page and len(text) < 1500:
        issues.append("aeo: thin page (<1500 chars) weakens answer recall")
    checked += 1
    llms = _read(root, "public/llms.txt")
    if llms is None:
        issues.append("aeo: missing llms.txt answer-engine map")
    elif "{{" in llms:
        issues.append("aeo: llms.txt has unresolved placeholders")
    checked += 1
    if brief:
        keywords = [k.strip().lower()
                    for k in str(brief.get("primary_keywords", "")).split(",")
                    if k.strip()]
        missing_kw = [k for k in keywords if k not in text.lower()]
        if missing_kw:
            issues.append(f"aeo: primary keywords absent: {missing_kw[:3]}")
    return {"ok": not issues, "issues": issues, "checked": checked}


def audit_geo(root: str, brief: dict | None = None) -> dict:
    """Generative-engine + geographic audit: machine-readable facts
    (llms.txt, JSON-LD, semantic landmarks), locale signals matching
    the brief's scope. Advisory."""
    issues: list[str] = []
    checked = 0
    page = _index_source(root)
    layout = _layout_source(root)
    combined = page + "\n" + layout
    checked += 1
    if "<main" not in combined:
        issues.append("geo: no <main> landmark for chunking")
    checked += 1
    if page.count("<h1") != 1:
        issues.append("geo: exactly one h1 required for topic focus")
    checked += 1
    if 'lang=' not in combined:
        issues.append("geo: no lang attribute for locale retrieval")
    checked += 1
    if brief:
        scope = str(brief.get("geographic_scope", "")).lower()
        if scope and "worldwide" not in scope and \
                "hreflang" not in combined:
            issues.append("geo: regional scope without hreflang")
    checked += 1
    if "application/ld+json" not in page:
        issues.append("geo: no JSON-LD facts for generators")
    return {"ok": not issues, "issues": issues, "checked": checked}


def audit_images(root: str) -> dict:
    """Image audit: alts, dimensions (no layout shift), local files
    exist, budgets, remote hosts listed. Advisory."""
    issues: list[str] = []
    checked = 0
    page = _index_source(root)
    if not page:
        return {"ok": False, "issues": ["images: index unreadable"],
                "checked": checked}
    imgs = re.findall(r"<img[^>]*>", page)
    checked += 1
    for tag in imgs:
        alt = re.search(r'alt="([^"]*)"', tag)
        if alt is None:
            issues.append("images: <img> without alt attribute")
            break
        if not re.search(r"(width|height)=" , tag) and not re.search(
                r"\b(h-|w-|aspect-)", tag):
            issues.append("images: <img> without dimensions (layout shift)")
            break
    else:
        checked += 1
    for tag in imgs:
        src = re.search(r'src="([^"]+)"', tag)
        if not src:
            continue
        url = src.group(1)
        if url.startswith(("data:", "blob:")):
            continue
        if url.startswith("http"):
            issues.append(f"images: remote image {url[:60]} (self-host?)")
            break
        local = os.path.join(root, url.lstrip("/").split("?")[0])
        if not os.path.isfile(local):
            issues.append(f"images: missing local file {url[:60]}")
            break
    else:
        checked += 1
    total = 0
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames
                       if d not in ("node_modules", "dist")]
        for name in filenames:
            if name.lower().endswith(
                    (".png", ".jpg", ".jpeg", ".webp", ".avif", ".svg")):
                try:
                    total += os.path.getsize(os.path.join(dirpath, name))
                except OSError:
                    pass
    checked += 1
    if total > (3 << 20):
        issues.append(f"images: {total} bytes over 3MiB image budget")
    checked += 1
    if _read(root, "public/og.svg") is None and \
            "og:image" in page:
        issues.append("images: og:image without local og asset")
    return {"ok": not issues, "issues": issues, "checked": checked}


def audit_link_safety(root: str) -> dict:
    """Link/frame hygiene: no javascript: URLs, remote forms stay
    local, external targets carry rel=noopener, no mixed http content.
    Findings are BLOCKING (injected into verify like secrets)."""
    issues: list[str] = []
    checked = 0
    page = _index_source(root)
    if not page:
        return {"ok": False, "issues": ["links: index unreadable"],
                "checked": checked}
    checked += 1
    if re.search(r"javascript:", page, re.I):
        issues.append("links: javascript: URL in page")
    for match in re.finditer(r"<a[^>]+>", page):
        tag = match.group(0)
        href = re.search(r'href="([^"]*)"', tag)
        if not href:
            continue
        url = href.group(1)
        if url.startswith("http") and 'rel="' not in tag:
            issues.append("links: external link without rel attribute")
            break
    else:
        checked += 1
    for match in re.finditer(r"<form[^>]*>", page):
        tag = match.group(0)
        action = re.search(r'action="([^"]*)"', tag)
        if action and action.group(1).startswith("http"):
            issues.append("links: form posts to remote origin")
            break
    else:
        checked += 1
    if re.search(r'src="http://(?!localhost|127\.0\.0\.1)', page):
        issues.append("links: mixed http:// content")
    return {"ok": not issues, "issues": issues, "checked": checked}


# --- Production page pass (faithful mode) --------------------------------------
#
# Applied to the faithful index after the static build: theme toggle
# (light/dark with saved preference), SEO head extras, external-link
# hardening. Each step is a pure file transform returning
# applied/skipped; failures never raise past the caller.

def inject_theme_toggle(site_root: str, palettes: dict) -> dict:
    """Add light/dark support to the faithful page: palette variables,
    pre-paint init script, persistent toggle button. The Stitch theme
    stays the dark design; :root carries the derived light palette
    with verified contrast (see build_palettes)."""
    try:
        with open(os.path.join(site_root, INDEX_REL)) as f:
            page = f.read()
    except OSError:
        return {"applied": [], "skipped": ["theme: index unreadable"]}
    if 'id="theme-toggle"' in page:
        return {"applied": [], "skipped": ["theme: already installed"]}
    if "<head" not in page or "<body" not in page:
        return {"applied": [], "skipped": ["theme: page shape unexpected"]}
    style = ("<style is:inline id=\"stitch-theme-vars\">\n" +
             palette_stylesheet(palettes) + THEME_TOGGLE_STYLE +
             light_mode_safety_css(list(palettes.get("dark", {}))) +
             "</style>")
    script = ('<script is:inline id="theme-init">\n' + THEME_INIT_SCRIPT +
              "\n</script>")
    page = re.sub(r"</head>", style + "\n" + script + "\n</head>", page,
                  count=1, flags=re.I)
    page = re.sub(r"(<body[^>]*>)",
                  r"\1\n" + THEME_TOGGLE_BUTTON, page, count=1, flags=re.I)
    try:
        with open(os.path.join(site_root, INDEX_REL), "w") as f:
            f.write(page)
    except OSError as e:
        return {"applied": [], "skipped": [f"theme: write failed ({e})"]}
    return {"applied": ["theme: light/dark toggle with saved preference"],
            "skipped": []}


def inject_seo_head(site_root: str, brief: dict, domain: str = "",
                    faithful: bool = False,
                    has_faq: bool = False) -> dict:
    """Insert description/canonical/social/JSON-LD into the faithful
    head. Absolute references exist only with --domain."""
    try:
        with open(os.path.join(site_root, INDEX_REL)) as f:
            page = f.read()
    except OSError:
        return {"applied": [], "skipped": ["seo-head: index unreadable"]}
    if 'name="description"' in page and "og:title" in page:
        # Partial prior run: a later run with --domain still completes
        # the absolute references instead of reporting nothing.
        canonical = absolute_url(domain, "/")
        if not canonical or "rel=\"canonical\"" in page:
            return {"applied": [], "skipped": ["seo-head: already present"]}
    extras = seo_head_extras(brief, domain, faithful, has_faq)
    tags = "\n".join(extras["tags"])
    for item in extras["jsonld"]:
        tags += ('\n<script type="application/ld+json">\n' +
                 json.dumps(item, ensure_ascii=False) + "\n</script>")
    if "</head>" not in page:
        return {"applied": [], "skipped": ["seo-head: no head element"]}
    page = page.replace("</head>", tags + "\n</head>", 1)
    # Absolute URLs only enter through --domain; if the page has no
    # head tags to attach to beyond </head>, canonical still needs the
    # domain. Guard against a stale no-domain run followed by a
    # domainful one: canonical is appended once.
    canonical = extras["canonical"]
    if canonical and "rel=\"canonical\"" not in page:
        page = page.replace("</head>",
                            f'<link rel="canonical" href="{canonical}" />\n'
                            "</head>", 1)
    try:
        with open(os.path.join(site_root, INDEX_REL), "w") as f:
            f.write(page)
    except OSError as e:
        return {"applied": [], "skipped": [f"seo-head: write failed ({e})"]}
    applied = ["seo-head: description, social cards, JSON-LD"]
    if extras["canonical"]:
        applied.append(f"seo-head: canonical {extras['canonical']}")
    return {"applied": applied, "skipped": extras["skipped"]}


def harden_links(site_root: str) -> dict:
    """Add rel=noopener to external links missing a rel attribute."""
    try:
        with open(os.path.join(site_root, INDEX_REL)) as f:
            page = f.read()
    except OSError:
        return {"applied": [], "skipped": ["links: index unreadable"]}

    hardenings = []

    def fix(match):
        tag = match.group(0)
        href = re.search(r'href="([^"]*)"', tag)
        if not href or not href.group(1).startswith("http"):
            return tag
        if "rel=" in tag:
            return tag
        hardenings.append(tag)
        # Insert right after the tag name: an appended `rel` lands
        # after the closing bracket and is not markup.
        head = re.match(r"<a\s*", tag)
        return tag[:head.end()] + 'rel="noopener" ' + tag[head.end():]

    fixed = re.sub(r"<a[^>]+>", fix, page)
    if not hardenings:
        return {"applied": [], "skipped": ["links: nothing to harden"]}
    try:
        with open(os.path.join(site_root, INDEX_REL), "w") as f:
            f.write(fixed)
    except OSError as e:
        return {"applied": [], "skipped": [f"links: write failed ({e})"]}
    return {"applied": [f"links: {len(hardenings)} external link(s) hardened"],
            "skipped": []}


def themed_tailwind_sources(site_root: str, config_js: str,
                            plugins: list[str]) -> dict:
    """Write the Tailwind build sources with CSS-variable indirection:
    tailwind.config.js (colors → var(--c-*)) plus an input stylesheet
    carrying :root (derived light) and html.dark (Stitch) palettes.
    Returns {"dark","light","contrast","written","error"}."""
    indirect, palette = var_indirect_config(config_js)
    if not palette:
        return {"dark": {}, "light": {}, "contrast": {}, "written": [],
                "error": "no colors found in Stitch theme"}
    palettes = build_palettes(palette)
    sources = write_tailwind_sources(site_root, indirect, plugins)
    if sources["error"]:
        return {"dark": {}, "light": {}, "contrast": {},
                "written": sources["written"],
                "error": sources["error"]}
    try:
        with open(os.path.join(site_root, "stitch.input.css"), "a") as f:
            f.write("\n/* Stitch palettes: :root light, html.dark "
                    "verbatim. */\n" + palette_stylesheet(palettes))
    except OSError as e:
        return {"dark": {}, "light": {}, "contrast": {},
                "written": sources["written"], "error": str(e)}
    return {"dark": palettes["dark"], "light": palettes["light"],
            "contrast": {f"{m}/{s}/{t}": r for (m, s, t), r in
                         palettes["contrast"].items()},
            "written": sources["written"], "error": ""}


# --- Design prompt (LLM-crafted) --------------------------------------------
#
# screen_prompt concatenates brief fields, which asked Stitch for a
# generic hero. A real design direction has to be composed: layout,
# palette, typography, and section order tuned to this business. The
# LLM reads the spec for that; the fallback keeps a gateway outage
# from stopping a run.
DESIGN_PROMPT_SYSTEM = (
    "You are a senior brand and product designer writing a UI "
    "generation brief. Reply with one dense paragraph of plain prose. "
    "No markdown, no headings, no lists, no quotes, no preamble, and "
    "no mention that you are an AI."
)

# The screen must arrive as HTML and CSS the journey can paste into a
# page. Without this ask Stitch sometimes returns an image-only screen
# (screenType IMAGE, htmlCode null), which cannot be copied at all.
DESIGN_PROMPT_TAIL = (
    " Deliver a complete, self-contained, responsive HTML landing page "
    "with inline CSS and Tailwind utility classes: a sticky header "
    "with a brand and at least four navigation links, a hero with one "
    "headline and one call to action, three or four feature or "
    "benefit sections, a short FAQ or process section, and a footer "
    "with secondary links. Use real text content, semantic landmarks, "
    "and accessible markup: aria-labels, visible focus styles, and a "
    "prefers-reduced-motion rule. Do not return an image, a "
    "wireframe, or a placeholder."
)

DESIGN_PROMPT_MAX_CHARS = 2600


def craft_design_prompt(llm, brief: dict, spec: str = "") -> str:
    """One inference call that composes the Stitch design brief.

    Bounded by construction: a single call, a length band that rejects
    markdown, JSON, and near-empty output, and a deterministic
    fallback so a gateway failure still yields a usable prompt.
    """
    from stitch import screen_prompt
    fallback = screen_prompt(brief) + DESIGN_PROMPT_TAIL
    if llm is None:
        return fallback
    content = (
        "Write a UI generation brief for one landing page.\n\n"
        f"BUSINESS:\n{json.dumps(brief, indent=2, sort_keys=True)}\n\n"
        f"SPECIFICATION:\n{(spec or 'not yet written')[:3000]}\n\n"
        "In one paragraph, describe the visual concept and the feeling "
        "it should create, the layout and section order from top to "
        "bottom, the colour direction with specific hex values for "
        "background, surface, text, and accent, the typography pairing "
        "with specific font names, and the photography or illustration "
        "direction. Name the business and its actual service. Be "
        "concrete enough for a designer who cannot ask questions. Do "
        "not invent statistics, prices, certifications, or customer "
        "names."
    )
    try:
        out = llm("design", [
            {"role": "system", "content": DESIGN_PROMPT_SYSTEM},
            {"role": "user", "content": content},
        ])
    except Exception:
        return fallback
    text = (out or "").strip()
    if not 200 <= len(text) <= 8000:
        return fallback
    if text.count("\n") > 6 or text.startswith(("{", "[", "<")):
        return fallback
    return text[:DESIGN_PROMPT_MAX_CHARS].rstrip() + DESIGN_PROMPT_TAIL
