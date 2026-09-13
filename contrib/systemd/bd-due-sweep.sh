#!/usr/bin/env bash
# One timer body: fire dues, then deliver seat notifies. That is the whole clock.
#
#   bd due sweep     → advances due_at, enqueues .beads/notify/
#   bd notify drain  → herdr-resolve assignee → prompt (retries next tick if offline)
#
# No second timer. No firstmate publish hook required for seat delivery.
set -euo pipefail

WORKSPACE=${BD_DUE_SWEEP_WORKSPACE:-}
BD=${BD_DUE_SWEEP_BD:-bd}
TIMEOUT=${BD_DUE_SWEEP_TIMEOUT:-120}
STATE=${BD_DUE_SWEEP_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/bd-due-sweep}
# Optional: still append a one-line summary somewhere (legacy). Seat delivery
# does not use this.
PUBLISH=${BD_DUE_SWEEP_PUBLISH:-}
DRAIN=${BD_DUE_SWEEP_DRAIN:-1}
DRAIN_LIMIT=${BD_DUE_SWEEP_DRAIN_LIMIT:-20}

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

exec 9>"$LOCK"
if ! flock -n 9; then
    echo "bd-due-sweep: previous sweep still running; skipping this tick"
    exit 0
fi

export PATH="${HOME:-}/.local/bin:${HOME:-}/.npm-global/bin:/usr/local/bin:/usr/bin:/bin:${PATH:-}"

report=$(cd "$WORKSPACE" && timeout "$TIMEOUT" "$BD" due sweep --json)
printf '%s\n' "$report" > "$REPORT_JSON.tmp"
mv -f "$REPORT_JSON.tmp" "$REPORT_JSON"

summary=$(printf '%s' "$report" | sed -n 's/.*"summary"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [ -z "$summary" ]; then
    echo "bd-due-sweep: sweep returned no summary" >&2
    exit 1
fi
echo "bd-due-sweep: $summary"

# ponytail: deliver in the same tick. Offline seats stay pending for next tick.
if [ "$DRAIN" != "0" ]; then
    (cd "$WORKSPACE" && "$BD" notify drain --all --limit "$DRAIN_LIMIT") || \
        echo "bd-due-sweep: notify drain had failures (non-fatal)" >&2
fi

# Optional legacy summary publisher (not seat delivery).
if [ -n "$PUBLISH" ]; then
    "$PUBLISH" "$summary" "$REPORT_JSON" || \
        echo "bd-due-sweep: publish hook failed (non-fatal)" >&2
fi

printf '%s\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$summary" > "$HEARTBEAT.tmp"
mv -f "$HEARTBEAT.tmp" "$HEARTBEAT"
