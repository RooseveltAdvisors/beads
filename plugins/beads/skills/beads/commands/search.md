---
description: Search issues by text query
argument-hint: "<query> [--status] [--label] [--assignee]"
---

Search issues across title, description, and ID with a simple text query.

**Note:** The `search` command is optimized for quick text searches and uses less context than `list` when accessed via MCP. For advanced filtering options, use `bd list`.

## Basic Usage

```bash
bd search "authentication bug"
bd search login --status open
bd search database --label backend
bd search "bd-5q"  # Search by partial issue ID
```

## How It Works

The search command finds issues where your query appears in **any** of:
- Issue title
- Issue description
- Issue ID (supports partial matching)
- Issue external ref

Unlike `bd list`, which requires you to specify which field to search, `bd search` automatically searches all text fields, making it faster and more intuitive for exploratory searches.

**ID queries vs free text.** A query is treated as an ID only when it names a bead: it has the shape of an issue ID (a prefix, then the last hyphen, then a hyphen-free identifier) AND that prefix is one this database actually uses. The database is the gate, not the spelling — hyphenated, underscored, long and uppercase prefixes (`claude-config-a3f8e9`, `bd_001-a3f8e9`) are ID queries wherever those prefixes exist. An ID query matches ID, title and external ref — not descriptions — so naming a bead returns that bead rather than every bead citing it. Ordinary hyphenated terms (`use-after-free`, `race-condition`, `rate-limit`) are free text and do search descriptions. An ID whose prefix this database does not use is indistinguishable from prose and is searched as free text, which still matches it by its ID text.

## Filters

- **--status, -s**: Filter by status (open, in_progress, blocked, closed)
- **--assignee, -a**: Filter by assignee
- **--type, -t**: Filter by type (bug, feature, task, epic, chore, decision)
- **--label, -l**: Filter by labels (must have ALL specified labels)
- **--label-any**: Filter by labels (must have AT LEAST ONE)
- **--limit, -n**: Limit number of results (default: 50)
- **--sort**: Sort by field: priority, created, updated, closed, status, id, title, type, assignee
- **--reverse, -r**: Reverse sort order
- **--long**: Show detailed multi-line output for each issue
- **--json**: Output results in JSON format

## Examples

### Basic Search
```bash
# Find all issues mentioning "auth" or "authentication"
bd search auth

# Search for performance issues
bd search performance --status open

# Find database-related bugs
bd search database --type bug
```

### Filtered Search
```bash
# Find open backend issues about login
bd search login --status open --label backend

# Search Alice's tasks for "refactor"
bd search refactor --assignee alice --type task

# Find recent bugs (limited to 10 results)
bd search bug --status open --limit 10
```

### Sorted Output
```bash
# Search bugs sorted by priority (P0 first)
bd search bug --sort priority

# Search features sorted by most recently updated
bd search feature --sort updated

# Search issues sorted by priority, lowest first
bd search refactor --sort priority --reverse
```

### JSON Output
```bash
# Get JSON results for programmatic use
bd search "api error" --json

# Use with jq for advanced filtering
bd search memory --json | jq '.[] | select(.priority <= 1)'
```

## Comparison with bd list

| Command | Best For | Default Limit | Context Usage |
|---------|----------|---------------|---------------|
| `bd search` | Quick text searches, exploratory queries | 50 | Low (efficient for LLMs) |
| `bd list` | Advanced filtering, precise queries | None | High (all results) |

**When to use `bd search`:**
- You want to find issues quickly by keyword
- You're exploring the issue database
- You're using an LLM/MCP and want to minimize context usage

**When to use `bd list`:**
- You need advanced filters (date ranges, priority ranges, etc.)
- You want all results without a limit
- You need special output formats (digraph, dot)
