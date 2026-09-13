# Beads notify outbox - delivery outside firstmate (2026-09-13)

## Thesis

Beads owns **time** (due/repeat/sweep) and **delivery** (seat-addressed outbox).
Firstmate, herdr, pi, and any LLM harness are optional **transports**, not the bus.

## Problem with the prior split

| Piece | Problem |
|---|---|
| `publish-wake-firstmate.sh` | Firstmate-shaped; couples every seat to FM |
| FM durable wake queue | Default rail for all seats → FM overload as agent count grows |
| herdr special-case for wiseman | Site glue, not beads |
| overseer-bead-id | Special case instead of assignee seat |

When FM wedges, everyone's due pings die even if beads and the timer are healthy.

## Design

```
bd due sweep
  → fire + reschedule + by_assignee
  → enqueue .beads/notify/outbox.jsonl  {seq, seat, issue_id, kind, title}
bd notify drain --seat <assignee> [--exec transport]
  → pending rows for seat → transport → ack
```

- **Seat** = `assignee` (empty → `unassigned`)
- **Outbox** = workspace-local under `.beads/notify/` (append-only JSONL + ack cursors)
- **Transports** = per-seat shell commands (`BD_NOTIFY_SEAT_WISEMAN=...`)
- **Clock** remains systemd `bd-due-sweep.timer` (unchanged D1 amendment)
- **Drain** is a separate short timer or After=sweep; drain lag ≠ clock lag

## Non-goals (v1)

- SQL table / dolt_ignored journal (file outbox is enough; promote if multi-writer needs it)
- assignee.required enforcement (separate PR; backfill first)
- Guaranteed exactly-once across hosts (per-workspace outbox)

## CLI

- `bd notify pending [--seat] [--json]`
- `bd notify drain --seat S [--limit N] [--exec CMD] [--no-ack]`
- `bd notify ack --seat S --upto N`
- `bd notify seats`

## Amendment to D5

D5 "route via fm-send / firstmate watcher" is demoted:

- Firstmate drains `assignee=firstmate` (and optionally unassigned→hold) like any seat
- Other seats do not traverse FM
- Stack-monitor still watches sweep liveness + pending outbox depth
