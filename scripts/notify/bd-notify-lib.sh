#!/usr/bin/env bash
# bd-notify-lib.sh — shared functions for the wiseman beads<->herdr notification wiring.
#
# Sourced by bin/bd-notify-pin, bin/bd-notify-deliver, bin/bd-notify-drain and
# bin/bd-comment-notify. Not meant to be executed directly.
#
# Model
# -----
# seats      beads assignee names (e.g. "arcs-fm", "wiseman"). Agents should run
#            bd with BEADS_ACTOR=<their seat> so self-comments never self-notify.
# pins       .beads/notify/pins.json : seat -> {session, pane_id, agent_session_id, harness, ...}
#            This is the file bd itself drains against ("bd notify drain").
# seat-map   .beads/notify/seat-map.json : optional explicit seat -> target hints
#            used by discovery before falling back to generic heuristics.
# delivery   follow-up (non-interrupting, queued for next turn boundary) vs
#            steer (immediate submit) — chosen per live harness, see
#            bin/bd-notify-deliver for the matrix.
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
bd_notify_seatmap_file() { printf '%s/seat-map.json\n' "$(bd_notify_dir)"; }

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

seatmap_get() { # $1 = seat -> seat-map hint object (empty if absent)
    local f
    f="$(bd_notify_seatmap_file)" || return 0
    [ -f "$f" ] || return 0
    _jq -e --arg s "$1" '.[$s] // empty' "$f" 2>/dev/null || true
}

# merge-write one seat's pin atomically (preserves other seats)
pin_write() { # $1 seat  $2 session  $3 pane_id  $4 agent_session_id  $5 harness  $6 note
    local seat="$1" session="$2" pane="$3" asid="${4:-}" harness="${5:-}" note="${6:-}"
    local file tmp
    file="$(bd_notify_pins_file)"
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

# ---------------------------------------------------------------------------
# seat discovery
# ---------------------------------------------------------------------------
# Prints: session \t pane_id \t agent_session_id \t harness   on success.
# Exit codes: 0 found; 5 no candidate; 6 ambiguous.

discover_seat() { # $1 = seat
    local seat="$1"
    local hint pick
    hint="$(seatmap_get "$seat")"
    pick="auto"
    if [ -n "$hint" ]; then
        pick="$(_jq -r '.pick // "auto"' <<<"$hint")"
    fi

    local repo_root
    repo_root="$(bd_notify_repo_root 2>/dev/null || true)"

    local filter_session="" filter_pane="" filter_name="" filter_title="" filter_cwd=""
    if [ -n "$hint" ]; then
        filter_session="$(_jq -r '.session // ""' <<<"$hint")"
        filter_pane="$(_jq -r '.pane // ""'     <<<"$hint")"
        filter_name="$(_jq -r '.name // ""'     <<<"$hint")"
        filter_title="$(_jq -r '.title // ""'   <<<"$hint")"
        filter_cwd="$(_jq -r '.cwd // ""'       <<<"$hint")"
    fi

    local sessions_to_scan=()
    if [ -n "$filter_session" ]; then
        sessions_to_scan=("$filter_session")
    else
        mapfile -t sessions_to_scan < <(herdr_running_sessions)
    fi

    local inventory
    inventory="$(herdr_inventory "${sessions_to_scan[@]}")"
    if [ -z "$inventory" ]; then
        echo "bd-notify: herdr reports no live agents" >&2
        return 5
    fi

    local seatsuffix="${seat##*-}"
    local candidates="" row
    local s pane asid kind status focused cwd title score depth
    while IFS= read -r row; do
        [ -n "$row" ] || continue
        IFS=$'\t' read -r s pane asid kind status focused cwd title <<<"$row"
        [ -n "$pane" ] || continue

        # hard filters from seat-map hint
        if [ -n "$filter_pane" ] && [ "$pane" != "$filter_pane" ]; then continue; fi
        if [ -n "$filter_name" ]; then
            if [ "${kind,,}" != "${filter_name,,}" ] && [ "${asid,,}" != "${filter_name,,}" ]; then
                continue
            fi
        fi
        if [ -n "$filter_title" ]; then
            if [[ "${title,,}" != *"${filter_title,,}"* ]]; then continue; fi
        fi
        if [ -n "$filter_cwd" ]; then
            if [[ "$cwd" != "$filter_cwd" && "$cwd" != "$filter_cwd"/* ]]; then continue; fi
        fi

        # soft conventions when no seat-map entry scoped this seat
        if [ -z "$hint" ]; then
            if [ "$seatsuffix" = "fm" ]; then
                if [ "$s" != "firstmate" ]; then continue; fi
                if [[ "${title,,}" != *"firstmate"* ]]; then continue; fi
            elif [ "$seatsuffix" = "agy" ] || [ "$seatsuffix" = "codex" ] || [ "$seatsuffix" = "cc" ]; then
                if [ "$kind" != "$seatsuffix" ]; then continue; fi
            fi
        fi

        depth="$(awk -F/ '{print NF-1}' <<<"$cwd")"
        score=$((40 - depth * 2))
        case "$status" in
            working) score=$((score + 4)) ;;
            idle)    score=$((score + 3)) ;;
            done)    score=$((score + 2)) ;;
            blocked) score=$((score + 0)) ;;
            *)       score=$((score + 1)) ;;
        esac
        if [ "$focused" = "focused" ]; then
            score=$((score + 10))
        fi
        if [ "$kind" = "$seat" ]; then
            score=$((score + 200))
        fi
        if [[ "${title,,}" == *"${seat,,}"* ]]; then
            score=$((score + 120))
        fi
        if [ -n "$repo_root" ]; then
            if [[ "$cwd" == "$repo_root" || "$cwd" == "$repo_root"/* ]]; then
                score=$((score + 60))
            fi
        fi

        case "$pick" in
            focused) if [ "$focused" != "focused" ]; then continue; fi ;;
            *)       : ;;
        esac

        candidates+="${score}"$'\t'"$s"$'\t'"$pane"$'\t'"$asid"$'\t'"$kind"$'\n'
    done <<<"$inventory"

    if [ -z "$candidates" ]; then
        echo "bd-notify: no live herdr pane matches seat '$seat'" >&2
        return 5
    fi

    local best b_score b_s b_pane b_asid b_kind ties
    best="$(_jq -Rn '
        [inputs | select(length > 0) | split("\t")]
        | sort_by(-(.[0] | tonumber))
        | .[0]' <<<"$candidates")"
    b_score="$(_jq -r '.[0]' <<<"$best")"
    b_s="$(_jq -r '.[1]'    <<<"$best")"
    b_pane="$(_jq -r '.[2]' <<<"$best")"
    b_asid="$(_jq -r '.[3]' <<<"$best")"
    b_kind="$(_jq -r '.[4]' <<<"$best")"

    # ambiguity guard: another candidate with the same score but a different pane
    if [ "$pick" = "auto" ]; then
        ties="$(_jq -Rn --arg score "$b_score" --arg pane "$b_pane" '
            [inputs | select(length > 0) | split("\t")]
            | map(select(.[0] == $score and .[2] != $pane)) | length' <<<"$candidates")"
        if [ "$ties" != "0" ]; then
            echo "bd-notify: seat '$seat' is ambiguous ($ties same-rank candidates); refusing to guess." \
                 "Add an entry to .beads/notify/seat-map.json." >&2
            return 6
        fi
    fi

    printf '%s\t%s\t%s\t%s\n' "$b_s" "$b_pane" "$b_asid" "$b_kind"
}
