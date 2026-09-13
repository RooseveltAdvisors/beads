#!/usr/bin/env bash
# Deprecated standalone wrapper. Drain is inlined in bd-due-sweep.sh.
# Kept so old units do not 404; prefer disabling bd-notify-drain.timer.
set -euo pipefail
WORKSPACE=${BD_NOTIFY_WORKSPACE:-${BD_DUE_SWEEP_WORKSPACE:-}}
BD=${BD_NOTIFY_BD:-${BD_DUE_SWEEP_BD:-bd}}
LIMIT=${BD_NOTIFY_LIMIT:-20}
[ -n "$WORKSPACE" ] || { echo "bd-notify-drain: set BD_NOTIFY_WORKSPACE" >&2; exit 2; }
cd "$WORKSPACE"
exec "$BD" notify drain --all --limit "$LIMIT"
