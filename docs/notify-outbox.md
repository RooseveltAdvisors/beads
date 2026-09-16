# Beads notify outbox - harness-agnostic seat delivery

## Thesis

Beads owns **time** (due/repeat/sweep) and **delivery ledger** (seat-addressed outbox).
Assignee is a stable **seat identity**. Drain delivers only to an exact pin
(`.beads/notify/pins.json`: herdr session + pane id, or agent session id).
There is no fuzzy search. A missing pin or a dead pinned target leaves the
row queued and marks the seat for parent escalation.

Firstmate is an orchestrator that may *host* harnesses; it is not a harness and
not the notify bus. No assignee gets a privileged code path in beads.

## Design

```
bd due sweep
  → fire + reschedule + by_assignee
  → enqueue .beads/notify/outbox.jsonl  {seq, seat, issue_id, kind, title}

bd notify drain --seat <assignee> [--exec transport]
  → pending rows for that seat → exact pin (or hold+escalate) → ack
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
- `bd notify resolve --seat S` — human diagnostic only (fuzzy lookalike is not delivery)

## Exact pin delivery

`bd notify drain` default transport:

1. Load `.beads/notify/pins.json` (seat → `{session, pane_id}` or `agent_session_id`).
   A fleet seed is in `examples/notify/pins.json`.
2. `herdr session list --json` → running sessions
3. `herdr --session S agent list` → live agents (any harness)
4. If the pinned target is among them, `herdr --session S agent prompt <pane> <text>`
5. Else hold the outbox row and write `.beads/notify/holds.json` so the parent escalates

A lookalike pane (same title, same session name, focused idle pi, …) never
receives the ping. `bd notify resolve` may still print a scored diagnostic
match; drain ignores it.

Supporting a new harness = herdr detecting it. Beads does not embed harness SDKs.

## Deployment (ponytail)

One systemd timer runs sweep then drain. No separate notify timer.
Pending outbox rows retry on the next sweep when the pinned target is live.
Unpinned or dead-pinned seats stay queued until a human (or lock owner) pins
the exact herdr id.


## Prompt playbook (finish-line education)

`notify.DefaultPrompt` always teaches how to end work in beads so harnesses
that only chat or write status files still learn the real finish line:

```text
bd comment <id> "what landed"
bd close <id> --reason "short real reason"
```

Kinds with stronger copy:

| Kind | When | Message emphasis |
|------|------|------------------|
| `due` / `defer` / `manual` | normal fires | short close footer |
| `stale-claim` | assigned open/in_progress gone quiet without close | full playbook: close / delta comment / reassign |
| `progress` | missing comment.progress trail | comment then close |
| `escalate` | escalations | show + comment + close if done |

`comment.progress_required` is mentioned so agents do not bare-close.
