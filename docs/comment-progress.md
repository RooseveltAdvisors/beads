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


## Stale in_progress (no close) wakes

If work is finished only in chat/status files, the close gate never runs. Due/stale-claim notify still wakes the seat. Prompt text (see `notify.DefaultPrompt`) teaches:

```text
bd comment <id> "what landed"
bd close <id> --reason "short real reason"
```

Kinds:
- `due` / `manual` / `defer` - short finish-line footer on every wake
- `stale-claim` - full playbook (close / delta comment / reassign)
- `progress` - missing progress trail while still open

Enqueue stale-claim/progress from clocks or ops (`bd notify` outbox); drain delivers via herdr to any harness.

## Stale-claim clock

Same timer as due sweep (`bd due sweep` → notify drain):

1. Scan assignee-set `in_progress` quieter than `comment.stale_claim_after` (house default `12h`)
2. Quiet anchor = latest of `started_at` and last `bd comment` time
3. Enqueue `kind: stale-claim` (skips ids due-fired this tick, pending rows, and recent stale-claim within the threshold)
4. Drain delivers the full finish-line playbook (`bd comment` / `bd close --reason`)

```bash
bd config set comment.stale_claim_after 12h   # or 0 / off to disable
bd progress stale --json                     # advisory list
```
