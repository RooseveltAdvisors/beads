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
- `bd notify resolve --seat S` - human diagnostic only (fuzzy lookalike is not delivery)

## Exact pin delivery

`bd notify drain` default transport:

1. Load `.beads/notify/pins.json` (seat → `{session, pane_id}` or `agent_session_id`).
   A fleet seed is in `examples/notify/pins.json`.
2. `herdr session list --json` → running sessions
3. `herdr --session S agent list` → live agents (any harness)
4. If the pinned target is among them, submit with that pane's **delivery mode**:
   - `follow_up` when the harness has a non-steer queue key (does not redirect the current turn)
   - `steer` (herdr `agent prompt` / Enter) as the fallback
5. Else hold the outbox row and write `.beads/notify/holds.json` so the parent escalates

The notify body (`notify.DefaultPrompt`) is the same in both modes. Only the submit key changes.

A lookalike pane (same title, same session name, focused idle pi, …) never
receives the ping. `bd notify resolve` may still print a scored diagnostic
match; drain ignores it.

Supporting a new harness = herdr detecting it, plus a follow-up key in
`internal/notify/delivery.go` when that harness actually has one. Beads does
not embed harness SDKs and does not depend on firstmate's event bus.

## Follow-up vs steer

Steer lands at the next tool boundary and can redirect in-flight work. Follow-up
waits until the current run is done. Drain prefers follow-up so a due/comment
ping does not yank the model mid-task.

| Harness (herdr `agent` label) | Mode | How |
|---|---|---|
| `pi`, `pi-signed` | `follow_up` | Option/Alt+Enter (`pane send-text` then `pane send-keys alt+enter`). Pi docs: Enter steers, Alt+Enter queues until the run finishes. |
| `cursor`, `cursor-agent` | `follow_up` | Tab queues after the turn on Cursor agent surfaces (Agents Window / queue). CLI Enter while busy is steer. |
| `codex` | `follow_up` | Tab queues for the next turn. Enter injects a steer after the current tool call. |
| `claude` (and `claude-code`) | `steer` | No follow-up submit distinct from Enter. Mid-run input is steer; a follow-up modifier is still an upstream feature request. |
| anything else herdr detects | `steer` | `herdr agent prompt` (text + Enter). |

JSON drain rows include `delivery_mode` (`follow_up` or `steer`). Human drain
lines print `session pane (harness/mode)`.

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
