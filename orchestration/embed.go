// Package orchestration embeds the Python orchestration runtime so the
// engine binary carries its agentic journey with it (spec.md §24,
// FR-036; ADR-0021).
//
// Embedding the Python sources here keeps one authoritative copy: the
// Go build cannot drift from the runtime it ships, and `repoforge
// generate` needs no checkout, no venv, and no path gymnastics. Test
// files, caches, and lockfiles are deliberately excluded: the runtime
// source plus the hash-pinned requirements are what a caller needs.
package orchestration

import "embed"

//go:embed agents.py brief.py graph.py llm.py mcp_client.py run.py state.py stitch.py requirements.txt
var Sources embed.FS

// MainModule is the journey entry point inside Sources.
const MainModule = "run.py"
