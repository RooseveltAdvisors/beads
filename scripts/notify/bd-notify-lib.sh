#!/usr/bin/env bash
# bd-notify-lib.sh — shared functions for the wiseman beads<->herdr notification wiring.
#
# Sourced by bin/bd-notify-pin and bin/bd-comment-notify. Not meant to be
# executed directly.
#
# Model
# -----
# seats      beads assignee names (e.g. "arcs-fm", "wiseman"). The speaker
#            (actor) is resolved by bd: --actor, BEADS_ACTOR, then the herdr
#            tab label, then git user.name. Self-comments (actor == assignee)
#            never self-notify; set BEADS_ACTOR only to override on purpose.
# pins       .beads/notify/pins.json : seat -> {session, pane_id, agent_session_id, harness, ...}
#            This is the file bd itself drains against ("bd notify drain").
#            Pins are written explicitly (bd-notify-pin set) and matched
#            exactly; nothing here guesses a target from titles, cwd or focus.
#            A repo may symlink this file at the machine-canonical map so one
#            seat resolves to one pane in every repo; pin_write/pin_delete
#            write through the link.
# delivery   `bd notify drain` owns the harness matrix (follow-up vs steer).
#
# Safety: this library never starts, kills or restarts any herdr session, pane
# or agent. It only reads agent lists, writes pins.json, and submits text to an
# already-running agent pane via the documented herdr contact commands.

set -euo pipefail

BD_NOTIFY_HERDR="${HERDR_BIN:-herdr}"

# ---------------------------------------------------------------------------
# repo / paths
# ---------------------------------------------------------------------------

bd_notify_repo_root() {
    if [ -n "${BD_NOTIFY_REPO_ROOT:-}" ]; then
        printf '%s\n' "$BD_NOTIFY_REPO_ROOT"
        return 0
    fi
    local root
    root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
    if [ -z "$root" ]; then
        if [ -d ".beads" ]; then
            root="$PWD"
        else
            echo "bd-notify: not inside a git repo and no .beads/ here" >&2
            return 1
        fi
    fi
    printf '%s\n' "$root"
}

bd_notify_dir() {
    printf '%s/.beads/notify\n' "$(bd_notify_repo_root)"
}

bd_notify_pins_file()    { printf '%s/pins.json\n'     "$(bd_notify_dir)"; }

# ---------------------------------------------------------------------------
# jq helpers
# ---------------------------------------------------------------------------

_jq() { command jq "$@"; }

pin_get() { # $1 = seat  -> pin object (empty if absent)
    local f
    f="$(bd_notify_pins_file)" || return 0
    [ -f "$f" ] || return 0
    _jq -e --arg s "$1" '.[$s] // empty' "$f" 2>/dev/null || true
}


# merge-write one seat's pin atomically (preserves other seats)
pin_write() { # $1 seat  $2 session  $3 pane_id  $4 agent_session_id  $5 harness  $6 note
    local seat="$1" session="$2" pane="$3" asid="${4:-}" harness="${5:-}" note="${6:-}"
    local file tmp
    file="$(bd_notify_pins_file)"
    # One machine-wide seat -> target map: a repo may symlink its pins.json at
    # the canonical file so the same seat resolves to the same pane everywhere.
    # mktemp + mv would replace the symlink itself and silently fork the map
    # again (that is how seat wiseman ended up with two different targets), so
    # resolve the link and write through it.
    if [ -L "$file" ]; then file="$(readlink -f "$file")"; fi
    mkdir -p "$(dirname "$file")"
    tmp="$(mktemp "$file.tmp.XXXXXX")"
    if [ -s "$file" ] && _jq -e . "$file" >/dev/null 2>&1; then
        _jq --arg s "$seat" --arg se "$session" --arg p "$pane" \
            --arg a "$asid" --arg h "$harness" --arg n "$note" \
            --arg t "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
            .[$s] = ((.[$s] // {})
                     + {session: $se, pane_id: $p, updated_at: $t})
                  | .[$s].agent_session_id = (if $a == "" then .[$s].agent_session_id // "" else $a end)
                  | .[$s].harness           = (if $h == "" then .[$s].harness // "" else $h end)
                  | .[$s].note              = (if $n == "" then .[$s].note // "" else $n end)' \
            "$file" > "$tmp"
    else
        _jq -n --arg s "$seat" --arg se "$session" --arg p "$pane" \
              --arg a "$asid" --arg h "$harness" --arg n "$note" \
              --arg t "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
            {($s): {session: $se, pane_id: $p, agent_session_id: $a,
                    harness: $h, note: $n, updated_at: $t}}' > "$tmp"
    fi
    mv "$tmp" "$file"
}

pin_delete() { # $1 seat
    local file tmp
    file="$(bd_notify_pins_file)" || return 0
    [ -f "$file" ] || return 0
    if [ -L "$file" ]; then file="$(readlink -f "$file")"; fi
    tmp="$(mktemp "$file.tmp.XXXXXX")"
    _jq 'del(.[$s])' --arg s "$1" "$file" > "$tmp"
    mv "$tmp" "$file"
}

# ---------------------------------------------------------------------------
# herdr discovery
# ---------------------------------------------------------------------------

herdr_running_sessions() {
    "$BD_NOTIFY_HERDR" session list --json 2>/dev/null \
        | _jq -r '.sessions[] | select(.running == true) | .name'
}

# JSON array of agent objects for one session.
# Handles both {"result":{"agents":[...]}} and {"agents":[...]} envelopes.
herdr_agents_json() { # $1 = session
    "$BD_NOTIFY_HERDR" --session "$1" agent list 2>/dev/null \
        | _jq '(.result.agents // .agents) // []'
}

# One normalized line per live agent across the given sessions:
#   session \t pane_id \t agent_session_id \t kind \t status \t focused \t cwd \t title
herdr_inventory() { # $@ = sessions (default: all running)
    local sessions=("$@")
    if [ ${#sessions[@]} -eq 0 ]; then
        mapfile -t sessions < <(herdr_running_sessions)
    fi
    local s
    for s in "${sessions[@]}"; do
        herdr_agents_json "$s" | _jq -r --arg s "$s" '
            .[] | [
                $s, .pane_id,
                (.agent_session.value // ""),
                (.agent // ""),
                (.agent_status // "unknown"),
                (if .focused == true then "focused" else "" end),
                (.foreground_cwd // .cwd // ""),
                (.terminal_title_stripped // .terminal_title // "")
            ] | @tsv' 2>/dev/null || true
    done
}

# Find the live agent row for a pinned target. Prints the inventory line.
find_live_row() { # $1 session  $2 pane_id  $3 agent_session_id
    local s="$1" pane="$2" asid="$3" row
    while IFS= read -r row; do
        [ -n "$row" ] || continue
        # shellcheck disable=SC2207  # tsv split via read below
        if [ "${row%%$'\t'*}" = "$s" ]; then
            local f_pane f_asid
            f_pane="$(cut -f2 <<<"$row")"
            f_asid="$(cut -f3 <<<"$row")"
            if { [ -n "$pane" ] && [ "$f_pane" = "$pane" ]; } \
               || { [ -n "$asid" ] && [ -n "$f_asid" ] && [ "$f_asid" = "$asid" ]; }; then
                printf '%s\n' "$row"
                return 0
            fi
        fi
    done < <(herdr_inventory "$s")
    return 1
}
