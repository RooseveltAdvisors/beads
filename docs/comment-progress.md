# Comment progress gate

Beads-owned close gate (same layer as due/notify - not firstmate).

## Policy (locked)

| Knob | Value |
|------|-------|
| Strength | Gate `bd close` / done-class closes |
| Freshness | Comment with `created_at >= started_at`, or non-empty close `--reason` |
| Scope | Issues with a non-empty assignee (events/wisps/templates exempt) |
| Default | ON in this house fork (`comment.progress_required: true`); upstream contrib should default OFF |

## Escape

```bash
bd close <id> --force-no-comment --reason "why progress was skipped"
```

`--force` still only bypasses blocker/open-child policy. It does **not** skip comment progress.

## Advisory

```bash
bd progress check <id...>
bd progress check <id...> --json
```

## Config

```yaml
# .beads/config.yaml
comment.progress_required: true   # house
# comment.progress_required: false  # recommended upstream default
```
