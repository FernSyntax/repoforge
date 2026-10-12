"""Intake brief: the world-class client questions.

Each question carries guidance so an operator (or a future intake UI)
knows what a good answer contains. Answers feed the specification,
the project model, and the content pass; missing answers pause the
run instead of being invented.
"""

from __future__ import annotations

QUESTIONS = [
    {
        "key": "project_name",
        "question": "What is the project name?",
        "help": "Lowercase alphanumerics and hyphens; seeds output paths.",
    },
    {
        "key": "purpose",
        "question": "What is the site for, in one plain sentence?",
        "help": "The single outcome a visitor should understand in five seconds.",
    },
    {
        "key": "ideal_customer_profile",
        "question": "Who is your ideal customer (ICP)?",
        "help": "Role, company size, context — who pays and why they care.",
    },
    {
        "key": "target_audience",
        "question": "Who else reads the site besides the ICP?",
        "help": "Secondary readers: evaluators, partners, hires, press.",
    },
    {
        "key": "tone_of_voice",
        "question": "How should the copy sound?",
        "help": "For example: plain and direct, warm and expert, bold and terse.",
    },
    {
        "key": "primary_keywords",
        "question": "Which search phrases must the copy serve?",
        "help": "Comma-separated; the phrases prospects actually type.",
    },
    {
        "key": "geographic_scope",
        "question": "Which geographies does the site serve?",
        "help": "Countries, regions, or worldwide; drives locale and example choices.",
    },
    {
        "key": "non_goals",
        "question": "What is explicitly out of scope?",
        "help": "What the site must not promise, claim, or become.",
    },
    {
        "key": "success_metric",
        "question": "What does success look like?",
        "help": "The one conversion or behavior change the site exists to cause.",
    },
]

REQUIRED = [q["key"] for q in QUESTIONS]


def missing_answers(brief: dict) -> list:
    """Keys with blank or absent answers, in question order."""
    return [k for k in REQUIRED if not str(brief.get(k, "")).strip()]


def questions_for(keys: list) -> list:
    """Full question records for the missing keys."""
    wanted = set(keys)
    return [q for q in QUESTIONS if q["key"] in wanted]
