#!/usr/bin/env bash
# BD_DUE_SWEEP_PUBLISH hook for a firstmate home: put one sweep summary on the
# durable wake queue.
#
# The append goes through firstmate's OWN helper (fm_wake_append), never a
# hand-rolled append to the queue file. The helper serializes on the queue
# lock, allocates the sequence number, and publishes the recovery marker the
# drain's acknowledgement depends on; a raw >> to the same file skips all
# three and corrupts the rail for every other producer.
set -euo pipefail

summary=${1:-}
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

# The key is epoch-unique so two ticks never collide on one queue key, and the
# payload is the single summary line the sweep already composed.
fm_wake_append check "beads-due:$(date +%s)" "check: beads-due $summary"
