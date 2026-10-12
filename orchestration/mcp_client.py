"""MCP stdio client: drives `repoforge mcp serve` over pipes.

Newline-delimited JSON-RPC 2.0, mirroring the server's dialect:
initialize handshake, tools/call with argument objects, protocol
errors raised as MCPError. The client owns the subprocess lifetime;
every call is independent and stateless, like the server.
"""

from __future__ import annotations

import json
import os
import subprocess

PROTOCOL = "2025-11-25"


class MCPError(Exception):
    pass


class Client:
    def __init__(self, binary: str, allowed_dir: str,
                 template_source: str = "", product_source: str = ""):
        cmd = [binary, "mcp", "serve", "--allowed-dir", allowed_dir]
        if template_source:
            cmd += ["--template-source", template_source]
        if product_source:
            cmd += ["--product-source", product_source]
        try:
            self.proc = subprocess.Popen(
                cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL, text=True, bufsize=1,
            )
        except OSError as e:
            raise MCPError(f"starting mcp server: {e}") from None
        self._next_id = 0
        hello = self._request("initialize", {
            "protocolVersion": PROTOCOL, "capabilities": {},
            "clientInfo": {"name": "repoforge-graph", "version": "0.2.0"},
        })
        if hello.get("protocolVersion") != PROTOCOL:
            self.close()
            raise MCPError(f"protocol mismatch: {hello.get('protocolVersion')}")
        # Drain the initialized notification (servers stay silent).
        self.proc.stdin.write(json.dumps(
            {"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
        self.proc.stdin.flush()

    def _request(self, method: str, params: dict) -> dict:
        self._next_id += 1
        self.proc.stdin.write(json.dumps(
            {"jsonrpc": "2.0", "id": self._next_id,
             "method": method, "params": params}) + "\n")
        self.proc.stdin.flush()
        line = self.proc.stdout.readline()
        if not line:
            raise MCPError("mcp server closed the stream")
        try:
            reply = json.loads(line)
        except ValueError as e:
            raise MCPError(f"unparseable reply: {e}") from None
        if reply.get("error"):
            err = reply["error"]
            raise MCPError(f"{err.get('code')}: {err.get('message')}")
        return reply.get("result", {})

    def call_tool(self, name: str, arguments: dict) -> dict:
        """Call a tool. Tool-level failures arrive as MCPError with
        the tool's JSON payload attached after a '| ' separator."""
        result = self._request("tools/call",
                               {"name": name, "arguments": arguments})
        content = result.get("content", [])
        text = "".join(b.get("text", "") for b in content
                       if b.get("type") == "text")
        if result.get("isError"):
            raise MCPError(f"tool {name} failed | {text}")
        try:
            return json.loads(text) if text else {}
        except ValueError as e:
            raise MCPError(f"tool {name} returned non-JSON: {e}") from None

    def close(self) -> None:
        try:
            if self.proc.stdin:
                self.proc.stdin.close()
        finally:
            self.proc.wait(timeout=10)

    @staticmethod
    def check_binary(explicit: str = "") -> str:
        """Resolve the engine binary: explicit path, REPOFFORGE_BIN,
        or PATH. Raises MCPError when absent so tests skip cleanly."""
        for candidate in (explicit, os.environ.get("REPOFFORGE_BIN", "")):
            if candidate and os.path.isfile(candidate) and os.access(candidate, os.X_OK):
                return candidate
        from shutil import which
        found = which("repoforge")
        if found:
            return found
        raise MCPError("no repoforge binary (set REPOFFORGE_BIN)")
