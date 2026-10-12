#!/usr/bin/env python3
"""Run the site-generation journey: start, resume, and status.

Threads persist in SQLite under the workdir; a paused run resumes
with the same thread id. The gateway key comes from FREELLMAPI_KEY;
no key is ever written to checkpoints or logs.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.types import Command

from graph import Deps, build_graph  # noqa: E402
from llm import Caller  # noqa: E402
from mcp_client import Client as MCPClient  # noqa: E402

EXIT_PAUSED = 3


def _graph(args, deps: Deps, db_path: str):
    conn = __import__("sqlite3").connect(db_path, check_same_thread=False)
    graph = build_graph(deps, checkpointer=SqliteSaver(conn))
    return graph


def _deps(args) -> Deps:
    routes = {}
    for item in args.route or []:
        task, _, model = item.partition("=")
        if task and model:
            routes[task] = model
    caller = Caller(routes=routes)
    binary = MCPClient.check_binary(args.repoforge_bin or "")
    allowed = os.path.abspath(args.workdir)

    def factory():
        return MCPClient(binary, allowed, args.template_source,
                         args.product_source)

    return Deps(llm=lambda task, messages: caller.complete(task, messages),
                mcp_factory=factory,
                allowed_root=allowed,
                site_rel=args.site_rel)


def cmd_start(args) -> int:
    with open(args.brief) as f:
        brief = json.load(f)
    if not isinstance(brief, dict):
        print("brief must be a JSON object", file=sys.stderr)
        return 2
    os.makedirs(args.workdir, exist_ok=True)
    run_id = args.run_id or ("run-" + uuid.uuid4().hex[:8])
    db_path = os.path.join(args.workdir, "runs.sqlite3")
    graph = _graph(args, _deps(args), db_path)
    config = {"configurable": {"thread_id": run_id}}
    initial = {"run_id": run_id, "brief": brief, "stage": "intake",
               "attempts": {}, "errors": []}
    outcome = _drive(graph, config, initial, as_json=args.json)
    if outcome == "paused":
        if not args.json:
            print(f"paused; resume with: run.py resume --workdir "
                  f"{args.workdir} --thread {run_id} "
                  f"--answers ANSWERS.json|--approval APPROVAL.json")
        return EXIT_PAUSED
    return 0


def _drive(graph, config, initial=None, as_json=False):
    """Run the graph until it pauses or finishes.

    With as_json the outcome is one JSON record on stdout —
    {"event": "paused"|"done"|"failed", "run_id", "payload", "report"}
    — so `repoforge generate` can drive the journey without scraping
    prose. Human output stays the default.
    """
    try:
        stream = graph.stream(initial, config, stream_mode="updates") \
            if initial is not None else graph.stream(None, config,
                                                     stream_mode="updates")
        for _ in stream:
            pass
    except Exception as e:  # a crashed node is a failure, not a traceback
        if as_json:
            print(json.dumps({"event": "failed", "run_id": "",
                              "report": "the journey crashed: "
                                        f"{type(e).__name__}: {e}"}))
            return "failed"
        print(f"the journey crashed: {type(e).__name__}: {e}", file=sys.stderr)
        return "failed"
    snapshot = graph.get_state(config)
    values = snapshot.values or {}
    tasks = snapshot.tasks
    run_id = str(values.get("run_id", "") or "")
    if tasks and getattr(tasks[0], "interrupts", None):
        payload = tasks[0].interrupts[0].value
        if as_json:
            print(json.dumps({"event": "paused", "run_id": run_id,
                              "payload": payload}))
            return "paused"
        print(json.dumps({"paused": payload}, indent=2))
        return "paused"
    if values.get("failed"):
        report = values.get("report", "failed with no report")
        if as_json:
            print(json.dumps({"event": "failed", "run_id": run_id,
                              "report": report}))
            return "failed"
        print(report, file=sys.stderr)
        return "failed"
    if values.get("done"):
        report = values.get("report", "done")
        if as_json:
            print(json.dumps({"event": "done", "run_id": run_id,
                              "report": report}))
            return "done"
        print(report)
        return "done"
    if as_json:
        print(json.dumps({"event": "failed", "run_id": run_id,
                          "report": "the run halted without a terminal "
                                    "state; inspect it with the status "
                                    "command"}))
        return "failed"
    print(json.dumps({"state": values}, indent=2, default=str))
    return "halted"


def cmd_resume(args) -> int:
    with open(args.payload) as f:
        envelope = json.load(f)
    if "answers" in envelope:
        resume_value = envelope["answers"]
    elif "approval" in envelope:
        resume_value = envelope["approval"]
    else:
        # A bare payload of answers (no envelope) is also accepted, so a
        # caller can write the answers object directly.
        resume_value = envelope
    db_path = os.path.join(args.workdir, "runs.sqlite3")
    graph = _graph(args, _deps(args), db_path)
    config = {"configurable": {"thread_id": args.thread}}
    outcome = _drive(graph, config, Command(resume=resume_value),
                     as_json=args.json)
    if outcome == "paused" and not args.json:
        print(f"paused again; resume with: run.py resume --workdir "
              f"{args.workdir} --thread {args.thread}")
    return EXIT_PAUSED if outcome == "paused" else 0


def cmd_status(args) -> int:
    import sqlite3
    db_path = os.path.join(args.workdir, "runs.sqlite3")
    conn = sqlite3.connect(db_path)
    from langgraph.checkpoint.sqlite import SqliteSaver
    graph = build_graph(_deps(args), checkpointer=SqliteSaver(conn))
    snapshot = graph.get_state({"configurable": {"thread_id": args.thread}})
    print(json.dumps(snapshot.values or {}, indent=2, default=str))
    return 0


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="cmd", required=True)
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--json", action="store_true",
                        help="emit one machine-readable event record "
                             "instead of human output")
    common.add_argument("--workdir", required=True)
    common.add_argument("--repoforge-bin", default="")
    common.add_argument("--template-source", default="template")
    common.add_argument("--product-source", default="product/astro")
    common.add_argument("--route", action="append", default=[],
                        help="task=model override, repeatable")
    start = sub.add_parser("start", parents=[common])
    start.add_argument("--brief", required=True)
    start.add_argument("--run-id", default="")
    start.add_argument("--site-rel", default="site")
    resume = sub.add_parser("resume", parents=[common])
    resume.add_argument("--thread", required=True)
    resume.add_argument("--payload", required=True)
    resume.add_argument("--site-rel", default="site")
    status = sub.add_parser("status", parents=[common])
    status.add_argument("--thread", required=True)
    status.add_argument("--site-rel", default="site")
    args = parser.parse_args(argv)
    if args.cmd == "start":
        return cmd_start(args)
    if args.cmd == "resume":
        return cmd_resume(args)
    return cmd_status(args)


if __name__ == "__main__":
    raise SystemExit(main())
