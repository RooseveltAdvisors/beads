#!/usr/bin/env bash
# Run one due sweep and publish its summary. The clock's whole body.
#
# Beads fires due beads lazily, on ready-work reads. That is a latency FLOOR,
# not a clock: a workspace nobody reads never fires anything, and a deadline
# nobody reads about is not a deadline. This script is the clock — a systemd
# timer runs it on a fixed cadence (see bd-due-sweep.timer).
#
# It stays deliberately DUMB. Every decision it could make is a decision that
# would then live in two places, drift, and be debugged at 3am from a timer
# log. It takes a lock, runs one bounded command, publishes one line, stamps a
# heartbeat, and exits. Anything cleverer belongs in `bd due sweep` itself,
# where it is testable.
#
# Configuration, all optional except the workspace:
#   BD_DUE_SWEEP_WORKSPACE  directory holding the .beads workspace (required)
#   BD_DUE_SWEEP_BD         path to the bd binary (default: bd on PATH)
#   BD_DUE_SWEEP_TIMEOUT    seconds the sweep may take (default: 120)
#   BD_DUE_SWEEP_STATE      directory for the lock and heartbeat
#                           (default: $XDG_STATE_HOME/bd-due-sweep)
#   BD_DUE_SWEEP_PUBLISH    command receiving:
#                             $1 = summary line
#                             $2 = path to last-report.json (full sweep JSON,
#                                  including by_assignee seat routing)
#                           Optional: with no publisher the sweep still runs
#                           and the summary goes to stdout (journal keeps it).
set -euo pipefail

WORKSPACE=${BD_DUE_SWEEP_WORKSPACE:-}
BD=${BD_DUE_SWEEP_BD:-bd}
TIMEOUT=${BD_DUE_SWEEP_TIMEOUT:-120}
STATE=${BD_DUE_SWEEP_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/bd-due-sweep}
PUBLISH=${BD_DUE_SWEEP_PUBLISH:-}

if [ -z "$WORKSPACE" ]; then
    echo "bd-due-sweep: BD_DUE_SWEEP_WORKSPACE is unset" >&2
    exit 2
fi
if [ ! -d "$WORKSPACE" ]; then
    echo "bd-due-sweep: workspace $WORKSPACE is not a directory" >&2
    exit 2
fi

mkdir -p "$STATE"
LOCK="$STATE/sweep.lock"
HEARTBEAT="$STATE/last-sweep"
REPORT_JSON="$STATE/last-report.json"

# A sweep that overruns its interval must not stack a second one on the same
# database. -n fails immediately rather than queueing: the next tick is soon,
# and a queue of sweeps waiting on a wedged one is how a clock becomes a fork
# bomb. Exit 0 — an overlap is the timer working, not a fault, and OnFailure=
# is reserved for real ones.
exec 9>"$LOCK"
if ! flock -n 9; then
    echo "bd-due-sweep: previous sweep still running; skipping this tick"
    exit 0
fi

# timeout bounds a sweep that hangs on a wedged database, so the failure is a
# failure (OnFailure= fires) instead of a timer unit stuck in `activating`
# forever, which looks alive to everything watching it.
report=$(cd "$WORKSPACE" && timeout "$TIMEOUT" "$BD" due sweep --json)

# Persist the full JSON (due_ids + by_assignee) so publishers and humans can
# inspect the last tick without re-running the sweep. Write-then-rename.
printf '%s\n' "$report" > "$REPORT_JSON.tmp"
mv -f "$REPORT_JSON.tmp" "$REPORT_JSON"

# One grep, because `bd due sweep` publishes the summary as a field: the line a
# human reads and the line the rail carries are the same string, and this
# script never reassembles it from counts.
summary=$(printf '%s' "$report" | sed -n 's/.*"summary"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [ -z "$summary" ]; then
    echo "bd-due-sweep: sweep returned no summary" >&2
    exit 1
fi

echo "bd-due-sweep: $summary"
if [ -n "$PUBLISH" ]; then
    # $1 = summary line (journal-friendly)
    # $2 = full report path (seat routing lives in by_assignee)
    "$PUBLISH" "$summary" "$REPORT_JSON"
fi

# The heartbeat is stamped LAST, so its mtime means "a sweep completed and
# published", not "a sweep started". A watchdog on an independent host reads
# this file's age: a clock that cannot say when it last ran is a clock nobody
# can trust. Write-then-rename so a reader never sees a half-written stamp.
printf '%s\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$summary" > "$HEARTBEAT.tmp"
mv -f "$HEARTBEAT.tmp" "$HEARTBEAT"
