#!/usr/bin/env bash
# Drain the beads notify outbox via herdr seat discovery.
#
# Beads owns time (sweep) and delivery ledger (.beads/notify/).
# This script only runs: bd notify drain --all
# which resolves each assignee seat to a live herdr agent instance (any
# harness herdr detects) and prompts it. No per-seat special cases.
#
# Configuration:
#   BD_NOTIFY_WORKSPACE   workspace with .beads (required)
#   BD_NOTIFY_BD          bd binary (default: bd)
#   HERDR_BIN             herdr binary (optional; bd notify discovers PATH)
#   BD_NOTIFY_LIMIT       max rows per seat (default: 20)
set -euo pipefail

WORKSPACE=${BD_NOTIFY_WORKSPACE:-}
BD=${BD_NOTIFY_BD:-bd}
LIMIT=${BD_NOTIFY_LIMIT:-20}

if [ -z "$WORKSPACE" ]; then
    echo "bd-notify-drain: BD_NOTIFY_WORKSPACE is unset" >&2
    exit 2
fi

cd "$WORKSPACE"
export PATH="${PATH:-/usr/bin:/bin}"
# Ensure user local bins for herdr/bd when run under systemd.
export PATH="${HOME:-}/.local/bin:${HOME:-}/.npm-global/bin:/usr/local/bin:/usr/bin:/bin:${PATH}"

echo "bd-notify-drain: draining all seats (limit=$LIMIT)"
"$BD" notify drain --all --limit "$LIMIT"
