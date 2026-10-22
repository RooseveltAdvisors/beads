#!/usr/bin/env bash
# bd-due-wake: the external clock the beads due-date invariant stands on.
#
# `bd due sweep --help` is explicit: beads fire lazily on ready-work reads,
# which is a latency floor, not a clock - "a workspace nobody reads never
# fires anything". This loop is the seam's clock: it sleeps until the earliest
# due_at across the managed workspaces, then runs the documented pair:
#
#   bd due sweep       fire the beads whose due date arrived
#   bd notify drain    deliver the outbox to each seat's pinned pane
#
# One service, no timer: the wake lands within MIN_SLEEP of the due instant
# instead of on a polling boundary. MAX_SLEEP only bounds recovery from
# suspend, held-row retries, and a bad next-due read.
#
# Ordered 2026-10-19 as bd-due-wake after the old bd-due-sweep timer was
# deleted by captain order (2026-10-12). This is the new design, not a
# restoration: no pinned binary copy (the deploy lane keeps ~/.local/bin/bd
# current), and exact-instant firing instead of a 5-minute poll.
set -uo pipefail

BD=${BD_DUE_WAKE_BD:-$HOME/.local/bin/bd}
WORKSPACES=${BD_DUE_WAKE_WORKSPACES:-"/opt/ra/firstmate $HOME/git/wiseman"}
MAX_SLEEP=${BD_DUE_WAKE_MAX_SLEEP:-300}
MIN_SLEEP=${BD_DUE_WAKE_MIN_SLEEP:-5}

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

# One clock at a time: a manual run must not overlap the service.
exec 9>"${XDG_RUNTIME_DIR:-/tmp}/bd-due-wake.lock"
flock -n 9 || { log "another bd-due-wake holds the lock; exiting"; exit 0; }

# next_due_epoch prints the earliest future due_at across the managed
# workspaces as an epoch, or "" when nothing is due. due_at values are RFC
# 3339; their lexicographic minimum is their chronological minimum.
next_due_epoch() {
  local ws due epoch best=""
  for ws in $WORKSPACES; do
    [ -d "$ws/.beads" ] || continue
    due=$(cd "$ws" && "$BD" ready --json 2>/dev/null |
      jq -r '[.[]|select(.due_at != null)|.due_at] | min // empty' 2>/dev/null)
    [ -n "$due" ] || continue
    epoch=$(date -d "$due" +%s 2>/dev/null) || continue
    if [ -z "$best" ] || [ "$epoch" -lt "$best" ]; then best=$epoch; fi
  done
  echo "$best"
}

log "bd-due-wake start: workspaces=$WORKSPACES binary=$BD"

while :; do
  for ws in $WORKSPACES; do
    [ -d "$ws/.beads" ] || continue
    if ! (cd "$ws" && "$BD" due sweep >/dev/null 2>&1); then
      log "due sweep failed in $ws"
    fi
    if ! (cd "$ws" && "$BD" notify drain --all --limit 200 >/dev/null 2>&1); then
      log "notify drain failed in $ws"
    fi
  done

  now=$(date +%s)
  next=$(next_due_epoch)
  if [ -n "$next" ] && [ "$next" -gt "$now" ]; then
    sleep_for=$((next - now))
  else
    # Nothing due, or a due time the sweep could not move: come back soon.
    sleep_for=$MIN_SLEEP
  fi
  [ "$sleep_for" -gt "$MAX_SLEEP" ] && sleep_for=$MAX_SLEEP
  [ "$sleep_for" -lt "$MIN_SLEEP" ] && sleep_for=$MIN_SLEEP

  log "next wake in ${sleep_for}s (earliest due_at: ${next:-none})"
  sleep "$sleep_for"
done
