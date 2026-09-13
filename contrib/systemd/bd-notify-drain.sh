#!/usr/bin/env bash
# Drain the beads notify outbox for every seat that has a transport configured.
#
# Beads owns time (bd due sweep) and delivery ledger (.beads/notify/).
# This script is a thin seat drain: it does not decide who is due; it only
# hands pending rows to per-seat transports.
#
# Configuration (environment):
#   BD_NOTIFY_WORKSPACE   workspace with .beads (required)
#   BD_NOTIFY_BD          bd binary (default: bd)
#   BD_NOTIFY_SEAT_<NAME> shell command template for seat NAME (uppercased,
#                         non-alnum -> _). Runs once per pending row with
#                         BD_NOTIFY_* env from `bd notify drain --exec`.
#                         Example:
#                           BD_NOTIFY_SEAT_WISEMAN='herdr --session wiseman agent prompt w1:p1 "$BD_NOTIFY_PROMPT"'
#                           BD_NOTIFY_SEAT_FIRSTMATE='fm_wake_append check "bd-notify:$BD_NOTIFY_ID" "check: bead $BD_NOTIFY_ID due"'
#   BD_NOTIFY_DEFAULT_SEAT_CMD
#                         fallback exec when a seat has no specific override
#   BD_NOTIFY_LIMIT       max rows per seat per tick (default: 20)
#
# Typical wiring: run after bd-due-sweep.service (or on its own short timer).
set -euo pipefail

WORKSPACE=${BD_NOTIFY_WORKSPACE:-}
BD=${BD_NOTIFY_BD:-bd}
LIMIT=${BD_NOTIFY_LIMIT:-20}

if [ -z "$WORKSPACE" ]; then
    echo "bd-notify-drain: BD_NOTIFY_WORKSPACE is unset" >&2
    exit 2
fi

cd "$WORKSPACE"

# List seats with pending work.
mapfile -t SEATS < <("$BD" notify seats --json 2>/dev/null | python3 -c '
import json,sys
try:
  seats=json.load(sys.stdin)
except Exception:
  seats=[]
for s in seats or []:
  print(s)
' 2>/dev/null || true)

if [ "${#SEATS[@]}" -eq 0 ]; then
    echo "bd-notify-drain: no seats in outbox"
    exit 0
fi

seat_env_key() {
    # wiseman -> BD_NOTIFY_SEAT_WISEMAN; portal-ops -> BD_NOTIFY_SEAT_PORTAL_OPS
    echo "BD_NOTIFY_SEAT_$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]' | tr -c 'A-Z0-9' '_')"
}

for seat in "${SEATS[@]}"; do
    pending=$("$BD" notify pending --seat "$seat" --json 2>/dev/null | python3 -c 'import json,sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null || echo 0)
    if [ "${pending:-0}" -eq 0 ]; then
        continue
    fi
    key=$(seat_env_key "$seat")
    # bash indirection for env var named in key
    exec_cmd=""
    eval "exec_cmd=\"\${$key:-}\""
    if [ -z "$exec_cmd" ]; then
        exec_cmd=${BD_NOTIFY_DEFAULT_SEAT_CMD:-}
    fi
    if [ -z "$exec_cmd" ]; then
        echo "bd-notify-drain: seat $seat has $pending pending, no transport configured ($key)"
        continue
    fi
    echo "bd-notify-drain: draining seat=$seat pending=$pending"
    "$BD" notify drain --seat "$seat" --limit "$LIMIT" --exec "$exec_cmd" || {
        echo "bd-notify-drain: seat $seat drain had failures (left pending)" >&2
    }
done
