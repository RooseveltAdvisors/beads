# Claude Code Entry Point for Beads

This file is intentionally short. Do not copy workflow, build, storage, or UI
rules here; those details drift quickly when repeated across agent entrypoints.

## Read First

- **Workflow and safety**: [AGENTS.md](AGENTS.md)
- **Detailed agent operations**: [AGENT_INSTRUCTIONS.md](AGENT_INSTRUCTIONS.md)
- **Architecture orientation**: [engdocs/CLAUDE.md](engdocs/CLAUDE.md)
- **PR maintenance policy**: [PR_MAINTAINER_GUIDELINES.md](PR_MAINTAINER_GUIDELINES.md)

## Current Ground Rules

- Run `bd prime` before doing tracked work.
- Follow `go.mod` and [AGENT_INSTRUCTIONS.md](AGENT_INSTRUCTIONS.md) for build
  and test commands; do not hard-code toolchain versions here.
- Beads uses Dolt as the issue database. Use `bd dolt push` / `bd dolt pull`
  for issue data sync; do not use export/import as a routine git workflow.
- The CLI Visual Design System lives in
  [AGENT_INSTRUCTIONS.md](AGENT_INSTRUCTIONS.md#visual-design-system).
- If this file conflicts with a linked source, trust the linked source and fix
  this file by removing the duplicate.

<!-- CODE-INTEL-ROUTING:START (managed by code-index.sh) -->
## Code-intelligence routing (this repo is indexed)
This repo has CodeGraph + codebase-memory-mcp indexes. **The agent self-routes by the question — never asks which tool:**
- call-flow / "how does X reach Y" / blast-radius before a change → **CodeGraph** (`codegraph_explore`, `impact`)
- worst functions / hotspots / dead code / complexity / architecture → **codebase-memory-mcp** (Cypher on `f.complexity`, `get_architecture`)
- an exact string, a file you know, a tiny/config/bash file, or freshness → **grep** (the floor)
- **Hybrid:** cbm finds the hotspot → CodeGraph scopes the blast radius → read the source to confirm.
Reindex is automatic (CodeGraph auto-syncs on save). Full governance: the `retrieval-routing-governance` rule.
<!-- CODE-INTEL-ROUTING:END -->
