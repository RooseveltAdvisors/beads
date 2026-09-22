# Beads notify outbox - harness-agnostic seat delivery

## Thesis

Beads owns **time** (due/repeat/sweep) and **delivery ledger** (seat-addressed outbox).
Assignee is a stable **seat identity**. Drain delivers only to an exact pin
(`.beads/notify/pins.json`: herdr session + pane id, or agent session id).
There is no fuzzy search. A missing pin, dead target, or blocked approval dialog leaves
the row queued and marks the seat for parent escalation.
Working targets without verified queue semantics hold until idle and deliver at the next turn boundary.

Firstmate is an orchestrator that may *host* harnesses; it is not a harness and
not the notify bus. No assignee gets a privileged code path in beads.

## Design

```
bd due sweep
  → fire + reschedule + by_assignee
  → enqueue .beads/notify/outbox.jsonl  {seq, seat, issue_id, kind, title}

bd notify drain --seat <assignee> [--exec transport]
  → pending rows for that seat → exact pin (or hold+escalate / hold-until-idle) → ack
```

- **Seat** = `assignee` (empty → `unassigned`)
- **Outbox** = workspace-local under `.beads/notify/`
- **Transports** = native Go herdr delivery (default), or custom `--exec`
- **Attribution & Self-Comment Skip** = actor from `BEADS_ACTOR` env, else `git config user.name`. Self-comments (actor == assignee) never enqueue outbox rows.
- **No special seats** in beads or contrib scripts

## Removed anti-patterns

- `publish-wake-*.sh` fan-out by assignee (e.g. hard-coded herdr session)
- Per-name env defaults for particular agents in fleet drop-ins
- Overseer-only bead id files as a second routing plane
- External `--exec` scripts required for standard non-interrupting queue semantics
- Self-ping feedback loops on comment updates (wiseman-c88)

Optional summary publishers may still append one sweep line to a local queue;
that is not per-bead seat delivery.

## CLI

- `bd notify pending [--seat] [--json]`
- `bd notify drain --seat S [--limit N] [--exec CMD] [--no-ack]`
- `bd notify ack --seat S --upto N`
- `bd notify seats`
- `bd notify resolve --seat S` - human diagnostic only (fuzzy lookalike is not delivery)

## Exact pin delivery

`bd notify drain` native transport:

1. Load `.beads/notify/pins.json` (seat → `{session, pane_id}` or `agent_session_id`).
   A fleet seed is in `examples/notify/pins.json`, and pins can be auto-discovered via `scripts/notify/bd-notify-pin`.
2. `herdr session list --json` → running sessions
3. `herdr --session S agent list` → live agents (any harness)
4. Check pinned target status and harness delivery profile:
   - **Target Blocked** (`status == blocked`): approval dialog open. Never type into it; hold row and mark seat for parent escalation (exit 8 semantics).
   - **Target Working** (`status == working`):
     - Verified follow-up queue: submit via harness queue method (`follow_up`).
     - Unverified queue semantics: hold until idle (`hold_until_idle`). Row stays queued, retrying at the next turn boundary without parent escalation.
   - **Target Idle / Done** (`status == idle | done`): submit immediately with Enter prompt (`steer` / `agent prompt`), trivially non-interrupting.
5. If the pinned target is missing or dead, hold the outbox row and mark `.beads/notify/holds.json` for parent escalation (exit 7 semantics).

The notify body (`notify.DefaultPrompt`) is identical across modes.

A lookalike pane (same title, same session name, focused idle pi, …) never
receives the ping. `bd notify resolve` may still print a scored diagnostic
match; drain ignores it.

## Delivery matrix (internal/notify/delivery.go)

| Harness (herdr `agent` label) | State | Delivery Action | How / Semantics |
|---|---|---|---|
| `pi`, `pi-signed` | `working` | `follow_up` | Option/Alt+Enter (`pane send-text` then `pane send-keys alt+enter`, with `ctrl+j` fallback). Queues for pi's next turn boundary, never interrupts in-flight work. |
| `cursor`, `cursor-agent` | `working` | `follow_up` | Tab queue (`pane send-text` then `pane send-keys tab`). Queues after turn on Cursor agent surfaces. |
| `codex` | `working` | `follow_up` | Tab queue (`pane send-text` then `pane send-keys tab`). Queues for next turn in Codex CLI. |
| `claude`, `claude-code` | `working` | `hold_until_idle` | **Deferred**: Enter mid-turn is steering. Row stays queued; delivers when target becomes idle. |
| `agy`, `antigravity` | `working` | `hold_until_idle` | **Deferred**: unverified queue semantics. Row stays queued; delivers when target becomes idle. |
| `gemini`, `kimi`, `omp`, `muse` | `working` | `hold_until_idle` | **Deferred**: unverified queue semantics. Row stays queued; delivers when target becomes idle. |
| Any unverified / unknown harness | `working` | `hold_until_idle` | **Deferred**: default to hold-until-idle until live-verified. |
| Any harness | `idle` / `done` | `steer` | Enter submit (`agent prompt`). Trivially non-interrupting since agent is not in-flight. |
| Any harness | `blocked` | **Escalate** | **Deferred**: approval dialog open. Never type into it; marks parent escalation (`holds.json`, exit 8). |
| Any seat | dead pin / no pin | **Escalate** | **Deferred**: target not live; marks parent escalation (`holds.json`, exit 7 / 5). |

JSON drain rows include `delivery_mode` (`follow_up`, `steer`, or `hold_until_idle`) and `classification`.

## Seat attribution and self-comment skip

Actor identity is resolved in priority order:
1. `--actor` CLI flag
2. `BEADS_ACTOR` environment variable (e.g. `export BEADS_ACTOR=arcs-fm`)
3. `git config user.name`
4. `$USER`

When a comment is added via `bd comment`:
- If `author == assignee`, `notify.CommentAssigneeSeat` returns `"", false` and skips enqueueing.
- If unassigned, skips enqueueing.
- Otherwise enqueues a comment notification for the assignee seat.

## Fleet tooling (`scripts/notify/`)

The repository includes helper scripts under `scripts/notify/`:
- `bd-notify-pin`: Seat discovery, pin inspection, refresh, and mapping against herdr.
- `fleet-monitor-beads-notify.sh`: Fleet auditor checking session integrity, outbox health, and non-interrupting delivery compliance.
- `bd-comment-notify`: Wrapper script for commenting and draining.
- `bd-notify-drain`: Shell drain wrapper with pin auto-discovery.
- `bd-notify-deliver`: Transport delivery reference.
- `test_beads_notify_e2e.sh`: Shell end-to-end integration test.

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
