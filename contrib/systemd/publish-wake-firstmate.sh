#!/usr/bin/env bash
# BD_DUE_SWEEP_PUBLISH hook for a firstmate home.
#
# Args:
#   $1  summary line from bd due sweep
#   $2  path to last-report.json (full sweep JSON, including by_assignee)
#
# Behavior:
#   1. Append one durable wake-queue row for firstmate (every tick).
#   2. Fan out each by_assignee seat:
#        - assignee=wiseman  → herdr --session wiseman agent prompt
#        - everything else   → already on the firstmate wake queue (step 1);
#                              firstmate drains and claims by seat.
#
# Seat routing comes FROM beads (`by_assignee` on the sweep report). This
# script does not re-derive assignees with per-id `bd show` when the report
# already grouped them. Per-id show is only used to load the prompt body.
set -euo pipefail

summary=${1:-}
report_json=${2:-}
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

if [ -z "$report_json" ]; then
    report_json="${XDG_STATE_HOME:-$HOME/.local/state}/bd-due-sweep/last-report.json"
fi
HERDR_BIN="${HERDR_BIN:-$(command -v herdr 2>/dev/null || true)}"
HERDR_BIN="${HERDR_BIN:-$HOME/.local/bin/herdr}"
BD_BIN="${BD_BIN:-$(command -v bd 2>/dev/null || true)}"
BD_BIN="${BD_BIN:-$HOME/.local/bin/bd}"

if [ ! -f "$report_json" ] || [ ! -x "$HERDR_BIN" ] || [ ! -x "$BD_BIN" ]; then
    exit 0
fi

# Pull wiseman seat ids from by_assignee (beads-native routing). Fall back to
# scanning due_ids only if an older bd without by_assignee produced the report.
mapfile -t WISEMAN_IDS < <(python3 - "$report_json" <<'PY'
import json, sys
try:
    rep = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(0)
ids = []
for seat in rep.get("by_assignee") or []:
    if str(seat.get("assignee") or "").strip().lower() == "wiseman":
        ids.extend(seat.get("ids") or [])
if not ids:
    # Pre-by_assignee bd: no seat map. Leave empty; do not guess.
    pass
for i in ids:
    i = str(i).strip()
    if i:
        print(i)
PY
)

if [ "${#WISEMAN_IDS[@]}" -eq 0 ]; then
    exit 0
fi

pane="$("$HERDR_BIN" --session wiseman agent list 2>/dev/null | python3 -c "
import sys,json
try:
  d=json.load(sys.stdin)
  agents=d.get('result',{}).get('agents') or d.get('agents') or []
except Exception:
  agents=[]
for a in agents:
  if a.get('agent')=='pi':
    print(a.get('pane_id','')); break
" 2>/dev/null || true)"
pane="${pane:-w1:p1}"

OVERSEER_ID_FILE="${XDG_STATE_HOME:-$HOME/.local/state}/bd-due-sweep/overseer-bead-id"
overseer_id=""
if [ -f "$OVERSEER_ID_FILE" ]; then
    overseer_id=$(tr -d '[:space:]' < "$OVERSEER_ID_FILE")
fi

for bid in "${WISEMAN_IDS[@]}"; do
    meta=$(cd "$FM_HOME" && "$BD_BIN" show "$bid" --json 2>/dev/null || true)
    [ -n "$meta" ] || continue
    title=$(printf '%s' "$meta" | python3 -c "import sys,json;d=json.load(sys.stdin);d=d[0] if isinstance(d,list) else d;print(d.get('title') or '')" 2>/dev/null || true)
    desc=$(printf '%s' "$meta" | python3 -c "import sys,json;d=json.load(sys.stdin);d=d[0] if isinstance(d,list) else d;print(d.get('description') or '')" 2>/dev/null || true)

    if [ -n "$overseer_id" ] && [ "$bid" = "$overseer_id" ]; then
        prompt="OVERSEER TICK (beads fm-driven, 30m). Bead ${bid} just fired. Run the bounded checklist, stamp /opt/ra/firstmate/state/wiseman-overseer.last-tick, go idle. Do not ask the captain.

${desc}"
    else
        prompt="WISEMAN BEAD TICK (assignee=wiseman). Bead ${bid} just fired: ${title}

${desc}

Do the work yourself. Stamp progress. Go idle. Do not ask the captain."
    fi

    if timeout 30 "$HERDR_BIN" --session wiseman agent prompt "$pane" "$prompt" >/dev/null 2>&1; then
        echo "publish-wake-firstmate: woke wiseman for $bid on $pane (by_assignee seat)"
    else
        echo "publish-wake-firstmate: wiseman prompt failed (non-fatal) bead=$bid pane=$pane" >&2
    fi
done
