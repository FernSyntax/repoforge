"""Stitch client and design-stage tests: mocked HTTP only.

No key, no network. Live calls stay operator-run; these tests pin the
contract, the failure taxonomy, redaction, bounds, and the skip path.
"""

import io
import json
import os
import sys
import urllib.error

import pytest

sys.path.insert(0, os.path.dirname(os.path.dirname(
    os.path.abspath(__file__))))

from stitch import (Client, StitchError, design_summary, screen_prompt,
                    screenshot_urls)
from graph import design_stage


class FakeHTTP:
    """urlopen stand-in: scripted bodies, headers, and errors."""

    def __init__(self, bodies=None, error=None, seen=None):
        self.bodies = list(bodies or [])
        self.error = error
        self.seen = seen if seen is not None else []

    def __call__(self, req, timeout=None):
        self.seen.append(req)
        if self.error is not None:
            raise self.error
        body = self.bodies.pop(0) if self.bodies else b"{}"
        return FakeResponse(body)


class FakeResponse:
    def __init__(self, body):
        self._body = body

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False

    def read(self, n=-1):
        return self._body


def http_error(code, body):
    return urllib.error.HTTPError("https://x", code, "err", {},
                                  io.BytesIO(body.encode()))


def run_client(fake, key="test-key"):
    import stitch as stitch_mod
    real = stitch_mod.urllib.request.urlopen
    stitch_mod.urllib.request.urlopen = fake
    try:
        return Client(api_key=key)
    finally:
        stitch_mod.urllib.request.urlopen = real


def test_list_tools_parses_envelope(monkeypatch):
    import stitch as stitch_mod
    body = json.dumps({"jsonrpc": "2.0", "id": 1,
                       "result": {"tools": [{"name": "a"}]}}).encode()
    fake = FakeHTTP([body])
    monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
    tools = Client(api_key="k").list_tools()
    assert [t["name"] for t in tools] == ["a"]


def test_sse_envelope_parses(monkeypatch):
    import stitch as stitch_mod
    inner = json.dumps({"jsonrpc": "2.0", "id": 1,
                        "result": {"tools": [{"name": "b"}]}})
    fake = FakeHTTP([f"event: message\ndata: {inner}\n\n".encode()])
    monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
    assert [t["name"] for t in Client(api_key="k").list_tools()] == ["b"]


def test_auth_header_and_bearer_absence(monkeypatch):
    import stitch as stitch_mod
    seen = []
    fake = FakeHTTP([b'{"jsonrpc":"2.0","id":1,"result":{}}'], seen=seen)
    monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
    Client(api_key="secret-key").list_tools()
    headers = dict(seen[0].headers)
    lowered = {k.lower(): v for k, v in headers.items()}
    assert lowered.get("x-goog-api-key") == "secret-key"
    assert "authorization" not in lowered


def test_failure_taxonomy(monkeypatch):
    import stitch as stitch_mod
    cases = [(401, "auth"), (403, "auth"), (429, "rate-limit"),
             (500, "server"), (503, "server")]
    for code, kind in cases:
        fake = FakeHTTP(error=http_error(code, '{"message":"nope"}'))
        monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
        with pytest.raises(StitchError) as exc:
            Client(api_key="k").list_tools()
        assert exc.value.kind == kind, (code, exc.value.kind)


def test_timeout_and_network(monkeypatch):
    import stitch as stitch_mod
    fake = FakeHTTP(error=TimeoutError("slow"))
    monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
    with pytest.raises(StitchError) as exc:
        Client(api_key="k").list_tools()
    assert exc.value.kind == "timeout"
    fake = FakeHTTP(error=OSError("down"))
    monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
    with pytest.raises(StitchError) as exc:
        Client(api_key="k").list_tools()
    assert exc.value.kind == "network"


def test_invalid_envelope(monkeypatch):
    import stitch as stitch_mod
    fake = FakeHTTP([b"not json at all"])
    monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
    with pytest.raises(StitchError) as exc:
        Client(api_key="k").list_tools()
    assert exc.value.kind == "invalid-output"


def test_redaction(monkeypatch):
    import stitch as stitch_mod
    fake = FakeHTTP(error=http_error(
        400, '{"message":"bad key secret-key-123"}'))
    monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
    with pytest.raises(StitchError) as exc:
        Client(api_key="secret-key-123").list_tools()
    assert "secret-key-123" not in str(exc.value)
    assert "[REDACTED]" in str(exc.value)


def test_missing_key_fails_before_request(monkeypatch):
    import stitch as stitch_mod
    monkeypatch.delenv("STITCH_API_KEY", raising=False)
    seen = []
    fake = FakeHTTP(seen=seen)
    monkeypatch.setattr(stitch_mod.urllib.request, "urlopen", fake)
    with pytest.raises(StitchError) as exc:
        Client(api_key="").call_tool("create_project", {"title": "t"})
    assert exc.value.kind == "missing-config"
    assert seen == []


class StubStitch:
    def __init__(self, calls=None, fail=None):
        self.calls = calls if calls is not None else []
        self.fail = fail
        self.closed = False

    def call_tool(self, name, arguments):
        self.calls.append((name, arguments))
        if self.fail == name:
            raise StitchError("server", "boom")
        if name == "create_project":
            return {"name": "projects/123"}
        if name == "generate_screen_from_text":
            return {"name": "projects/123/screens/abc"}
        if name == "get_screen":
            return {"files": [
                {"downloadUrl": "https://x/shot.png",
                 "mimeType": "image/png"},
                {"downloadUrl": "https://x/code.html",
                 "mimeType": "text/html"},
            ], "designSystem": {"colors": {"primary": "#123456"}}}
        raise AssertionError(name)

    def close(self):
        self.closed = True


def brief():
    return {"project_name": "demo", "purpose": "Demo.",
            "target_audience": "Ops.", "ideal_customer_profile": "Ops.",
            "tone_of_voice": "Plain.", "primary_keywords": "demo",
            "geographic_scope": "Everywhere.", "non_goals": "None.",
            "success_metric": "Done."}


def test_design_stage_success():
    from graph import Deps
    stub = StubStitch()
    deps = Deps(stitch_factory=lambda: stub)
    out = design_stage(deps, {"run_id": "r1", "brief": brief()})
    assert out["stage"] == "project_model"
    assert out["design"]["project"] == "projects/123"
    assert out["design"]["screen"] == "projects/123/screens/abc"
    assert out["design"]["screenshots"] == ["https://x/shot.png"]
    assert out["design"]["tokens"] == {"colors.primary": "#123456"}
    assert [n for n, _ in stub.calls] == ["create_project",
                                          "generate_screen_from_text",
                                          "get_screen"]
    assert stub.closed


def test_design_stage_skips_without_client():
    from graph import Deps
    out = design_stage(Deps(), {"run_id": "r1", "brief": brief()})
    assert out == {"stage": "project_model",
                   "design": {"skipped": "no-stitch-client"}}


def test_design_stage_failure_skips_never_fails():
    from graph import Deps
    stub = StubStitch(fail="generate_screen_from_text")
    deps = Deps(stitch_factory=lambda: stub)
    out = design_stage(deps, {"run_id": "r1", "brief": brief()})
    assert out["stage"] == "project_model"
    assert out["design"]["skipped"] == "stitch-failed: server"


def test_screen_prompt_deterministic():
    first = screen_prompt(brief())
    assert first == screen_prompt(brief())
    assert "demo" in first and "Plain." in first


def test_screenshot_urls_filters_and_dedups():
    doc = {"files": [
        {"downloadUrl": "https://x/a.png", "mimeType": "image/png"},
        {"downloadUrl": "https://x/a.png", "mimeType": "image/png"},
        {"downloadUrl": "https://x/b.html", "mimeType": "text/html"},
        {"nope": 1},
    ]}
    assert screenshot_urls(doc) == ["https://x/a.png"]
    assert screenshot_urls({}) == []
    assert screenshot_urls(None) == []


def test_design_summary_empty_on_skip():
    assert design_summary({}) == ""
    assert design_summary({"skipped": "x"}) == ""
    text = design_summary({"project": "p", "screenshots": ["a", "b"],
                           "tokens": {"c": "1"}})
    assert "p" in text and "2 reference render(s)" in text
