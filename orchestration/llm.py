"""Gateway client: OpenAI-compatible chat completions over HTTPS.

Stdlib only (urllib): the provider abstraction lives in Go
(internal/provider); this client is the thin Python edge the graph
uses for the content pass, with the same rules — Bearer unified key
from env, classified failures, bounded single attempts (retry budgets
belong to graph nodes, not to this call).
"""

from __future__ import annotations

import json
import os
import urllib.request
import urllib.error

ENV_KEY = "FREELLMAPI_KEY"
ENV_BASE_URL = "FREELLMAPI_BASE_URL"
DEFAULT_BASE_URL = "http://localhost:3001/v1"
DEFAULT_ROUTE = "auto"
DEFAULT_TIMEOUT = 60


class GatewayError(Exception):
    """Classified gateway failure. Never carries the credential."""

    def __init__(self, kind: str, message: str, status: int = 0):
        super().__init__(f"{kind}: {message}")
        self.kind = kind
        self.status = status


AUTH = "auth"
RATE_LIMIT = "rate-limit"
SERVER = "server"
TIMEOUT = "timeout"
OUTPUT = "invalid-output"
EMPTY = "empty-response"
RETRYABLE = {"rate-limit", "server", "timeout"}


def classify(status: int) -> str:
    if status in (401, 403):
        return AUTH
    if status == 429:
        return RATE_LIMIT
    if status >= 500:
        return SERVER
    return "invalid-request"


class Caller:
    """One configured gateway caller. Routes resolve logical tasks to
    model routes; unlisted tasks use the router default."""

    def __init__(self, base_url: str = "", api_key: str = "",
                 routes: dict | None = None, timeout: int = DEFAULT_TIMEOUT):
        self.base_url = (base_url or os.environ.get(ENV_BASE_URL)
                         or DEFAULT_BASE_URL).rstrip("/")
        self.api_key = api_key or os.environ.get(ENV_KEY, "")
        self.routes = dict(routes or {})
        self.timeout = timeout

    def route_for(self, task: str) -> str:
        return self.routes.get(task) or DEFAULT_ROUTE

    def complete(self, task: str, messages: list) -> str:
        """One bounded text completion. Raises GatewayError."""
        if not self.api_key:
            raise GatewayError(AUTH, f"missing gateway credential (set {ENV_KEY})")
        body = json.dumps({
            "model": self.route_for(task),
            "messages": messages,
        }).encode()
        req = urllib.request.Request(
            self.base_url + "/chat/completions", data=body,
            headers={"Content-Type": "application/json",
                     "Authorization": "Bearer " + self.api_key},
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                payload = resp.read(1 << 20)
        except urllib.error.HTTPError as e:
            raw = e.read(1 << 16).decode("utf-8", "replace")
            raise GatewayError(classify(e.code), _message(raw, e.code)) from None
        except TimeoutError as e:
            raise GatewayError(TIMEOUT, f"request timed out: {e}") from None
        except OSError as e:
            raise GatewayError("network", f"request failed: {e}") from None
        try:
            data = json.loads(payload)
        except ValueError as e:
            raise GatewayError(OUTPUT, f"decoding response: {e}") from None
        choices = data.get("choices") or []
        content = (choices[0].get("message", {}).get("content", "")
                   if choices else "")
        if not content.strip():
            raise GatewayError(EMPTY, "gateway returned no usable content")
        return content


def _message(raw: str, status: int) -> str:
    try:
        data = json.loads(raw)
    except ValueError:
        return raw.strip() or f"status {status}"
    if isinstance(data, dict):
        msg = data.get("message") or data.get("error")
        if isinstance(msg, str) and msg.strip():
            return msg.strip()
    return f"status {status}"
