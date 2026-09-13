#!/usr/bin/env bash
# OPTIONAL legacy hook: one sweep summary line onto a firstmate wake queue.
# Not required for seat delivery (bd notify handles that). Safe to leave unset.
set -euo pipefail
summary=${1:-}
[ -n "$summary" ] || exit 2
FM_HOME=${FM_HOME:-/opt/ra/firstmate}
# shellcheck source=/dev/null
. "$FM_HOME/bin/fm-wake-lib.sh"
fm_wake_append check "beads-due:$(date +%s)" "check: beads-due $summary"
