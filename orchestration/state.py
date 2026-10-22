"""Run state contract for the LangGraph journey.

The state is runtime-agnostic JSON: digests and references travel,
never credentials and never generated file contents. The Go runner
uses the same shape (internal/orchestrate); a future adapter can
drive either side.
"""

from __future__ import annotations

from typing import TypedDict


class Approval(TypedDict, total=False):
    action: str
    artifact: str
    by: str


class RunState(TypedDict, total=False):
    run_id: str
    brief: dict
    spec: str
    spec_digest: str
    spec_critique: str
    design: dict
    project_doc: dict
    out_dir: str
    vars: dict
    stage: str
    attempts: dict
    approval_rounds: int
    repair_rounds: int
    content_done: bool
    generated: bool
    verified: bool
    agent_reports: dict
    warnings: list
    errors: list
    report: str
    done: bool
    failed: bool


FINAL_STAGES = ("done", "failed")
