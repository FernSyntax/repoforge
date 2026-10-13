"""Visual-audit tests: mocked fetch and model throughout.

No key, no network, no browser. The live vision path stays
operator-run; these tests pin request shapes, bounds, finding
validation, skip paths, and the advisory semantic.
"""

import io
import json
import os
import sys
import urllib.error

sys.path.insert(0, os.path.dirname(os.path.dirname(
    os.path.abspath(__file__))))

from llm import (GatewayError, fetch_image, image_part,
                 MAX_IMAGE_BYTES)
from graph import _parse_findings, visual_stage


class FakeHTTP:
    def __init__(self, body=None, error=None):
        self.body = body or b""
        self.error = error

    def __call__(self, req, timeout=None):
        if self.error is not None:
            raise self.error
        return FakeResponse(self.body)


class FakeResponse:
    def __init__(self, body):
        self._body = body

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return False

    def read(self, n=-1):
        return self._body


def http_error(code):
    return urllib.error.HTTPError("https://x", code, "err", {},
                                  io.BytesIO(b"nope"))


def test_image_part_shape():
    part = image_part("QUJD", "image/png")
    assert part == {"type": "image_url",
                    "image_url": {"url": "data:image/png;base64,QUJD"}}


def test_fetch_image_success_and_cap(monkeypatch):
    import llm as llm_mod
    monkeypatch.setattr(llm_mod.urllib.request, "urlopen",
                        FakeHTTP(b"\x89PNG" + b"\x00" * 100))
    data, mime = fetch_image("https://x/shot.png", api_key="k")
    assert mime == "image/png"
    import base64
    assert base64.b64decode(data).startswith(b"\x89PNG")


def test_fetch_image_oversize(monkeypatch):
    import llm as llm_mod
    monkeypatch.setattr(llm_mod.urllib.request, "urlopen",
                        FakeHTTP(b"\x00" * (MAX_IMAGE_BYTES + 8)))
    try:
        fetch_image("https://x/big.png")
    except GatewayError as e:
        assert e.kind == "invalid-request"
    else:
        raise AssertionError("oversize image must fail")


def test_fetch_image_taxonomy(monkeypatch):
    import llm as llm_mod
    cases = [(401, "auth"), (429, "rate-limit"), (503, "server")]
    for code, kind in cases:
        monkeypatch.setattr(llm_mod.urllib.request, "urlopen",
                            FakeHTTP(error=http_error(code)))
        try:
            fetch_image("https://x/a.png")
        except GatewayError as e:
            assert e.kind == kind, (code, e.kind)
        else:
            raise AssertionError(f"status {code} must fail")
    monkeypatch.setattr(llm_mod.urllib.request, "urlopen",
                        FakeHTTP(error=TimeoutError("slow")))
    try:
        fetch_image("https://x/a.png")
    except GatewayError as e:
        assert e.kind == "timeout"
    else:
        raise AssertionError("timeout must fail")


def test_parse_findings_table():
    good, err = _parse_findings(json.dumps({"findings": [
        {"severity": "major", "area": "contrast", "note": "Body text fails."},
        {"severity": "info", "area": "spacing", "note": "Fine."},
    ]}))
    assert err == "" and len(good) == 2
    assert good[0]["severity"] == "major"
    for bad in ('{"nope": 1}', '[]', '{"findings": "x"}',
                '{"findings": [{"severity": "huge", "note": "x"}]}',
                '{"findings": [{"severity": "minor", "note": "  "}]}',
                '{"findings": [{"severity": "minor"}]}',
                '{"findings": ["x"]}',
                "not json"):
        _, err = _parse_findings(bad)
        assert err != "", bad
    many = {"findings": [{"severity": "info", "area": "a",
                          "note": f"n{i}"} for i in range(30)]}
    capped, err = _parse_findings(json.dumps(many))
    assert err == "" and len(capped) == 10


class FakeDeps:
    def __init__(self, llm=None):
        self._llm = llm

    def llm(self, task, messages):
        return self._llm(task, messages)


def test_visual_skips_without_screenshots():
    out = visual_stage(FakeDeps(), {"run_id": "r", "brief": {}})
    assert out == {"stage": "verify",
                   "visual": {"skipped": "no-screenshots"}}
    out = visual_stage(FakeDeps(), {"run_id": "r", "brief": {},
                                    "design": {"skipped": "x"}})
    assert out["visual"]["skipped"] == "no-screenshots"


def test_visual_records_findings_and_majors(monkeypatch):
    import llm as llm_mod
    seen = {}

    def fake_fetch(url, api_key="", timeout=60):
        seen["url"] = url
        seen["key"] = api_key
        return "QUJD", "image/png"

    monkeypatch.setattr(llm_mod, "fetch_image", fake_fetch)
    def llm(task, messages):
        assert task == "design-review"
        parts = messages[1]["content"]
        assert any(p.get("type") == "image_url" for p in parts)
        return json.dumps({"findings": [
            {"severity": "major", "area": "contrast",
             "note": "Hero text unreadable."},
            {"severity": "minor", "area": "spacing", "note": "Tight."},
        ]})

    deps = FakeDeps(llm=llm)
    state = {"run_id": "r", "brief": {},
             "design": {"screenshots": ["https://x/1.png",
                                        "https://x/2.png"]}}
    out = visual_stage(deps, state)
    assert out["stage"] == "verify"
    assert out["visual"]["majors"] == ["Hero text unreadable."]
    assert len(out["visual"]["findings"]) == 2
    assert seen["url"] == "https://x/1.png"


def test_visual_fetch_failure_skips(monkeypatch):
    import llm as llm_mod

    def boom(url, api_key="", timeout=60):
        raise GatewayError("timeout", "slow")

    monkeypatch.setattr(llm_mod, "fetch_image", boom)
    deps = FakeDeps(llm=lambda t, m: (_ for _ in ()).throw(
        AssertionError("llm must not run")))
    out = visual_stage(deps, {"run_id": "r", "brief": {},
                               "design": {"screenshots": ["https://x/1.png"]}})
    assert out["visual"]["skipped"] == "fetch-failed: timeout"


def test_visual_invalid_model_output_skips(monkeypatch):
    import llm as llm_mod
    monkeypatch.setattr(llm_mod, "fetch_image",
                        lambda *a, **k: ("QUJD", "image/png"))
    deps = FakeDeps(llm=lambda t, m: "not json")
    out = visual_stage(deps, {"run_id": "r", "brief": {},
                               "design": {"screenshots": ["https://x/1.png"]}})
    assert out["visual"]["skipped"].startswith("invalid-findings")


def test_visual_llm_outage_skips(monkeypatch):
    import llm as llm_mod
    monkeypatch.setattr(llm_mod, "fetch_image",
                        lambda *a, **k: ("QUJD", "image/png"))

    def down(task, messages):
        raise GatewayError("rate-limit", "exhausted")

    out = visual_stage(FakeDeps(llm=down), {"run_id": "r", "brief": {},
                        "design": {"screenshots": ["https://x/1.png"]}})
    assert out["visual"]["skipped"] == "review-failed: rate-limit"
