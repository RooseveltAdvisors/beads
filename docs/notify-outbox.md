# Beads notify outbox - harness-agnostic seat delivery (2026-09-13)

## Thesis

Beads owns **time** (due/repeat/sweep) and **delivery ledger** (seat-addressed outbox).
Assignee is a stable **seat identity**. Live agent instances (any harness) are
resolved later by generic drain/discovery - not by special-casing names like
firstmate or wiseman in beads core.

Firstmate is an orchestrator that may *host* harnesses; it is not a harness and
not the notify bus. No assignee gets a privileged code path in beads.

## Design

```
bd due sweep
  → fire + reschedule + by_assignee
  → enqueue .beads/notify/outbox.jsonl  {seq, seat, issue_id, kind, title}

bd notify drain --seat <assignee> [--exec transport]
  → pending rows for that seat → optional transport → ack
```

- **Seat** = `assignee` (empty → `unassigned`)
- **Outbox** = workspace-local under `.beads/notify/`
- **Transports** = generic (`--exec`, or `BD_NOTIFY_SEAT_<NAME>` / `BD_NOTIFY_DEFAULT_SEAT_CMD`)
- **No special seats** in beads or contrib scripts

## Removed anti-patterns

- `publish-wake-*.sh` fan-out by assignee (e.g. hard-coded herdr session)
- Per-name env defaults for particular agents in fleet drop-ins
- Overseer-only bead id files as a second routing plane

Optional summary publishers may still append one sweep line to a local queue;
that is not per-bead seat delivery.

## CLI

- `bd notify pending [--seat] [--json]`
- `bd notify drain --seat S [--limit N] [--exec CMD] [--no-ack]`
- `bd notify ack --seat S --upto N`
- `bd notify seats`

## Next (not this change)

Seat directory + live instance discovery (herdr agent list, any harness) so
drain targets agent instances by assignee without static pane commands.
