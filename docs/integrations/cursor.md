---
title: Cursor
description: Set up beads for Cursor with a project rules file, the Beads skill, and lifecycle hooks
---

Use Beads with Cursor through a project rules file that keeps the workflow
guidance in front of the agent every turn, plus hooks that refresh it.

```bash
bd setup cursor            # Project rules, skill, and hooks
bd setup cursor --global   # Global skill and hooks
bd setup cursor --check
```

`bd setup cursor` installs:

| File | Purpose |
|------|---------|
| `.cursor/rules/beads.mdc` | Always-applied rule with the canonical Beads workflow guidance; Cursor re-includes it every turn |
| `.agents/skills/beads/` | The shared Beads agent skill (the same one Codex uses) |
| `.cursor/hooks.json` | Runs `bd cursor-hook` on session start, before compaction, and after tool use |

The hooks call `bd`, so make sure `bd` is on Cursor's `PATH`. With
`--global`, the skill and hooks go to `~/.agents/skills/beads/` and
`~/.cursor/hooks.json`; Cursor has no file-based global rules location, so
add global rules through Cursor Settings.

Restart Cursor after installing so the rule and hooks load.

## Verifying it works

```bash
bd setup cursor --check
```

In a session, the agent should know your bd workflow without being told — ask
it what work is ready and it should run `bd ready`. If it doesn't, confirm
`.cursor/rules/beads.mdc` exists and restart Cursor.

After a context compaction, have the agent run `bd prime` to restore the
workflow context, ready work, and project memories.

## Remove

```bash
bd setup cursor --remove
```

This removes the rules file and the Beads hook entries while leaving the rest
of `.cursor/` intact. The shared skill stays in place if the Codex integration
still uses it.

## Related

- [IDE Setup](/getting-started/ide-setup) — all editor integrations
- [Claude Code](/integrations/claude-code)
- [Codex](/integrations/codex)
