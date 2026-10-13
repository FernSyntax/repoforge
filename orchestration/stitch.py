"""Stitch design client: Streamable-HTTP JSON-RPC over the official
endpoint (ADR-0022).

Verified 2026-10-13 against the live `tools/list`: 15 tools over
`POST /mcp` accepting plain JSON-RPC with an `accept` header. Auth is
an API key header from `STITCH_API_KEY` only — never logged, never
committed, rotated immediately if ever pasted. Failures are
classified; remote calls are bounded (one project and at most two
screens per run by default); nothing here deletes remote state.
Tests and CI use recorded shapes, never live calls.
"""

from __future__ import annotations

import json
import os
import urllib.request
import urllib.error

ENDPOINT = "https://stitch.googleapis.com/mcp"
ENV_KEY = "STITCH_API_KEY"
DEFAULT_TIMEOUT = 90
MAX_SCREENS_PER_RUN = 1

AUTH = "auth"
RATE_LIMIT = "rate-limit"
SERVER = "server"
TIMEOUT = "timeout"
NETWORK = "network"
INVALID = "invalid-output"
CONFIG = "missing-config"
RETRYABLE = {"rate-limit", "server", "timeout", "network"}


class StitchError(Exception):
    """Classified Stitch failure. Never carries the credential."""

    def __init__(self, kind: str, message: str, status: int = 0):
        super().__init__(f"{kind}: {message}")
        self.kind = kind
        self.status = status


def _redact(text: str, key: str) -> str:
    if key and key in text:
        return text.replace(key, "[REDACTED]")
    return text


class Client:
    """One configured Stitch endpoint. Construct with a key or rely on
    the environment; missing keys fail fast before any request."""

    def __init__(self, api_key: str = "", endpoint: str = "",
                 timeout: int = DEFAULT_TIMEOUT):
        self.api_key = api_key or os.environ.get(ENV_KEY, "")
        self.endpoint = (endpoint or os.environ.get("STITCH_ENDPOINT")
                         or ENDPOINT).rstrip("/")
        self.timeout = timeout
        self._next_id = 0

    def _headers(self) -> dict:
        headers = {"Content-Type": "application/json",
                   "Accept": "application/json, text/event-stream"}
        if self.api_key:
            headers["X-Goog-Api-Key"] = self.api_key
        return headers

    def _post(self, payload: dict) -> dict:
        raw = json.dumps(payload).encode()
        req = urllib.request.Request(
            self.endpoint, data=raw, headers=self._headers(),
            method="POST",
        )
        try:
            with urllib.request.urlopen(req,
                                        timeout=self.timeout) as resp:
                body = resp.read(4 << 20)
        except urllib.error.HTTPError as e:
            raise self._classified(e.code, e.read(1 << 16)) from None
        except TimeoutError as e:
            raise StitchError(TIMEOUT, f"request timed out: {e}") from None
        except OSError as e:
            raise StitchError(NETWORK, f"request failed: {e}") from None
        return self._decode(body)

    def _decode(self, body: bytes) -> dict:
        text = body.decode("utf-8", "replace")
        try:
            return json.loads(text)
        except ValueError:
            pass
        # Streamable HTTP may frame the payload as server-sent events.
        for line in text.splitlines():
            line = line.strip()
            if line.startswith("data:"):
                try:
                    return json.loads(line[len("data:"):].strip())
                except ValueError:
                    continue
        raise StitchError(INVALID, "unparseable response envelope")

    def _classified(self, status: int, raw: bytes) -> StitchError:
        text = _redact(raw.decode("utf-8", "replace"), self.api_key)
        message = text.strip() or f"status {status}"
        try:
            data = json.loads(text)
        except ValueError:
            data = None
        if isinstance(data, dict):
            err = data.get("error") or {}
            if isinstance(err, dict) and err.get("message"):
                message = str(err["message"])
            elif isinstance(data.get("message"), str):
                message = data["message"]
        if status in (401, 403):
            return StitchError(AUTH, message, status)
        if status == 429:
            return StitchError(RATE_LIMIT, message, status)
        if status >= 500:
            return StitchError(SERVER, message, status)
        return StitchError("invalid-request", message, status)

    def call_tool(self, name: str, arguments: dict):
        """Call one tool. Protocol errors raise; tool-level failures
        return the envelope for the caller to interpret."""
        if not self.api_key:
            raise StitchError(
                CONFIG, f"missing Stitch credential (set {ENV_KEY})")
        self._next_id += 1
        reply = self._post({"jsonrpc": "2.0", "id": self._next_id,
                            "method": "tools/call",
                            "params": {"name": name,
                                       "arguments": arguments or {}}})
        if isinstance(reply, dict) and reply.get("error"):
            err = reply["error"]
            raise StitchError("tool-failed",
                              f"{name}: {err.get('message', err)}")
        return (reply.get("result", {}) if isinstance(reply, dict)
                else {})

    def list_tools(self) -> list:
        self._next_id += 1
        reply = self._post({"jsonrpc": "2.0", "id": self._next_id,
                            "method": "tools/list", "params": {}})
        if isinstance(reply, dict) and reply.get("error"):
            raise StitchError("tool-failed", str(reply["error"]))
        result = reply.get("result", {}) if isinstance(reply, dict) else {}
        return result.get("tools", [])


def screen_prompt(brief: dict) -> str:
    """Deterministic brief-to-prompt rendering: the same brief always
    asks for the same screen, so runs stay comparable."""
    return (
        f"Marketing landing page hero screen for {brief.get('project_name')}: "
        f"{brief.get('purpose')} "
        f"Audience: {brief.get('target_audience')} "
        f"({brief.get('ideal_customer_profile')}). "
        f"Tone: {brief.get('tone_of_voice')}. "
        f"Keywords: {brief.get('primary_keywords')}. "
        f"Geography: {brief.get('geographic_scope')}. "
        f"Exclusions: {brief.get('non_goals')}."
    )


def screenshot_urls(screen: dict) -> list:
    """Image file URLs from a get_screen asset, for downstream visual
    review. URLs may require the API key to fetch; callers pass it as
    a header, never embedded in logs."""
    urls = []
    if not isinstance(screen, dict):
        return urls
    for key in ("files", "assets"):
        items = screen.get(key) or []
        if isinstance(items, dict):
            items = list(items.values())
        for item in items:
            if not isinstance(item, dict):
                continue
            url = item.get("downloadUrl")
            mime = str(item.get("mimeType", ""))
            if url and ("image" in mime or url.endswith((".png", ".jpg"))):
                urls.append(url)
    seen, out = set(), []
    for url in urls:
        if url not in seen:
            seen.add(url)
            out.append(url)
    return out


def design_summary(design: dict) -> str:
    """Compact, prompt-safe direction line from a design record. Empty
    when there is nothing to say, so callers stay unchanged."""
    if not design or design.get("skipped"):
        return ""
    parts = []
    if design.get("project"):
        parts.append(f"Stitch project {design['project']}")
    tokens = design.get("tokens") or {}
    if isinstance(tokens, dict) and tokens:
        shown = ", ".join(f"{k}={v}" for k, v in list(tokens.items())[:8])
        parts.append(f"design tokens: {shown}")
    if design.get("screenshots"):
        parts.append(f"{len(design['screenshots'])} reference render(s)")
    return "; ".join(parts)
