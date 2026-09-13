#!/usr/bin/env bash
# BD_DUE_SWEEP_PUBLISH hook (optional): one sweep summary line onto a durable
# wake queue. Per-bead seat delivery is beads-owned (bd notify outbox + drain),
# not this script. Do not special-case assignees here.
set -euo pipefail

summary=${1:-}
# $2 may be last-report.json; unused here on purpose (outbox is the seat rail).
if [ -z "$summary" ]; then
    echo "publish-wake-firstmate: no summary given" >&2
    exit 2
fi

FM_HOME=${FM_HOME:-/opt/ra/firstmate}
LIB="$FM_HOME/bin/fm-wake-lib.sh"
if [ ! -f "$LIB" ]; then
    echo "publish-wake-firstmate: $LIB missing; cannot publish" >&2
    exit 1
fi

STATE=${STATE:-$FM_HOME/state}
export FM_HOME STATE
# shellcheck source=/dev/null
. "$LIB"

fm_wake_append check "beads-due:$(date +%s)" "check: beads-due $summary"
