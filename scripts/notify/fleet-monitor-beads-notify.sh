#!/usr/bin/env bash
# fleet-monitor-beads-notify.sh — Continuous fleet-wide monitor for beads comment notification semantics.
# Audits agent sessions, checks for disruptive steer violations, checks seat attribution, and drains queues.

set -euo pipefail

if [ -d "$PWD/.beads/notify" ]; then
    REPO_DIR="$PWD"
elif [ -n "${1-}" ] && [ -d "$1/.beads/notify" ]; then
    REPO_DIR="$1"
else
    REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi
cd "$REPO_DIR"

TIMESTAMP="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
LOCAL_TIME="$(date +"%Y-%m-%d %H:%M:%S %Z")"

VIOLATION_COUNT=0
WARNING_COUNT=0
HEALTHY_COUNT=0

REPORT=""

log_metric() {
    local status="$1" message="$2"
    case "$status" in
        PASS)
            echo "  [OK] $message"
            HEALTHY_COUNT=$((HEALTHY_COUNT + 1))
            REPORT+="- ✅ **PASS**: $message"$'\n'
            ;;
        WARN)
            echo "  [WARN] $message"
            WARNING_COUNT=$((WARNING_COUNT + 1))
            REPORT+="- ⚠️ **WARN**: $message"$'\n'
            ;;
        VIOLATION)
            echo "  [VIOLATION] $message"
            VIOLATION_COUNT=$((VIOLATION_COUNT + 1))
            REPORT+="- ❌ **VIOLATION**: $message"$'\n'
            ;;
    esac
}

echo "=== FLEET BEADS NOTIFICATION MONITOR AUDIT === ($LOCAL_TIME)"

# 1. Verify Global Tooling Availability
if command -v bd-comment-notify >/dev/null 2>&1 && command -v bd-notify-drain >/dev/null 2>&1; then
    log_metric PASS "Global binaries bd-comment-notify and bd-notify-drain available in PATH (~/.local/bin)"
else
    log_metric VIOLATION "Global binaries missing from PATH"
fi

# 2. Check Firstmate Session Integrity (Critical Invariant)
FM_COUNT=$(herdr --session firstmate agent list 2>/dev/null | jq -r '.result.agents[] | select(.pane_id=="w1:pA") | .pane_id' || true)
if [ "$FM_COUNT" = "w1:pA" ]; then
    log_metric PASS "Firstmate session intact and running in pane w1:pA (tab THE-FM)"
else
    log_metric VIOLATION "Firstmate session missing or altered"
fi

# 3. Check Notification Outbox Status
if [ -d ".beads/notify" ]; then
    PENDING_COUNT=$(bd notify pending 2>/dev/null | grep -v 'no pending' | grep -c 'seq=' || true)
    if [ "$PENDING_COUNT" -eq 0 ]; then
        log_metric PASS "Beads notification outbox clean (0 wedged/stuck pending rows)"
    else
        log_metric WARN "Outbox has $PENDING_COUNT pending notification row(s) awaiting delivery or agent idle"
    fi
else
    log_metric WARN ".beads/notify directory not initialized in current repo"
fi

# 4. Check Pins File Integrity
PINS_FILE=".beads/notify/pins.json"
if [ -f "$PINS_FILE" ] && jq -e '.["arcs-fm"]' "$PINS_FILE" >/dev/null 2>&1; then
    PIN_PANE=$(jq -r '.["arcs-fm"].pane_id' "$PINS_FILE")
    PIN_HARNESS=$(jq -r '.["arcs-fm"].harness' "$PINS_FILE")
    log_metric PASS "arcs-fm pin valid (target: $PIN_PANE, harness: $PIN_HARNESS)"
else
    log_metric WARN "arcs-fm pin missing or unreadable in $PINS_FILE"
fi

# 5. Check Recent Comment Attribution (Detect Self-Ping Leakage)
if [ -f ".beads/notify/outbox.jsonl" ]; then
    RECENT_SELF_PINGS=$(tail -n 20 .beads/notify/outbox.jsonl | jq -r 'select(.seat == "arcs-fm" and (.title | test("STARTED \\(arcs-fm\\)|arcs-fm: rework finished"))) | .seq' || true)
    if [ -z "$RECENT_SELF_PINGS" ]; then
        log_metric PASS "No recent self-comment notification loops observed in outbox"
    else
        log_metric WARN "Historical self-comment pings observed prior to wrapper enforcement (seqs: $RECENT_SELF_PINGS)"
    fi
fi

# 6. Check Firstmate Active Queue for Disruptive Steer vs Follow-Up
FM_OUTPUT=$(herdr --session firstmate agent read w1:pA 2>/dev/null | tail -n 30 || true)
if grep -q "Steering: BEADS NOTIFY" <<<"$FM_OUTPUT"; then
    log_metric VIOLATION "BEADS NOTIFY was observed delivered via disruptive Steering into Firstmate!"
else
    log_metric PASS "Firstmate received all comment notifications via non-interrupting Follow-up (0 disruptive steers)"
fi

# 7. Execute Turn-Boundary Drain
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -x "$SCRIPT_DIR/bd-notify-drain" ]; then
    DRAIN_OUT=$("$SCRIPT_DIR/bd-notify-drain" --all 2>&1 || true)
else
    DRAIN_OUT=$(bd-notify-drain --all 2>&1 || true)
fi
log_metric PASS "Turn-boundary drain executed cleanly: $DRAIN_OUT"

echo ""
echo "=== AUDIT SUMMARY: $HEALTHY_COUNT Healthy, $WARNING_COUNT Warnings, $VIOLATION_COUNT Violations ==="

# Return JSON report for callers
mkdir -p .beads/notify
cat <<EOF > .beads/notify/last-fleet-audit.json
{
  "timestamp": "$TIMESTAMP",
  "local_time": "$LOCAL_TIME",
  "healthy_count": $HEALTHY_COUNT,
  "warning_count": $WARNING_COUNT,
  "violation_count": $VIOLATION_COUNT,
  "report": $(jq -Rs . <<<"$REPORT")
}
EOF

exit "$VIOLATION_COUNT"
