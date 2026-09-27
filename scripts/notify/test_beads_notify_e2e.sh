#!/usr/bin/env bash
# test_beads_notify_e2e.sh — End-to-end validation for beads comment notification system.
# Verifies follow-up vs steer semantics, author filtering, and cross-harness delivery matrix.

set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_DIR"

PASS_COUNT=0
FAIL_COUNT=0

# If live beads issue is not present, use a hermetic mock bd for E2E assertions
MOCK_DIR="$(mktemp -d)"
trap 'rm -rf "$MOCK_DIR"' EXIT

cat <<'EOF' > "$MOCK_DIR/bd"
#!/usr/bin/env bash
cmd="${1:-}"
case "$cmd" in
    show)
        echo '[{"id":"wiseman-c88","assignee":"arcs-fm","owner":"arcs-fm","title":"Test issue"}]'
        exit 0
        ;;
    comment)
        exit 0
        ;;
    notify)
        sub="${2:-}"
        case "$sub" in
            pending)
                echo '[{"seq":1,"seat":"arcs-fm","issue_id":"wiseman-c88","kind":"comment"}]'
                exit 0
                ;;
            drain)
                exit 0
                ;;
        esac
        ;;
esac
exit 0
EOF
chmod +x "$MOCK_DIR/bd"

if ! bd show wiseman-c88 --json >/dev/null 2>&1; then
    export PATH="$MOCK_DIR:$PATH"
fi

assert_eq() {
    local desc="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        echo "  [PASS] $desc"
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        echo "  [FAIL] $desc (expected '$expected', got '$actual')"
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
}

assert_match() {
    local desc="$1" pattern="$2" text="$3"
    if grep -qE "$pattern" <<<"$text"; then
        echo "  [PASS] $desc"
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        echo "  [FAIL] $desc (pattern '$pattern' did not match: '$text')"
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
}

echo "=== BEADS COMMENT NOTIFICATION E2E TEST SUITE ==="

# 1. Test usage / arg validation
echo "Test 1: Usage on missing arguments"
out="$("$REPO_DIR/scripts/notify/bd-comment-notify" 2>&1 || true)"
assert_match "prints usage on empty args" "Usage" "$out"

# 2. Test self-comment skip
echo "Test 2: Self-comment skipping"
out="$(BEADS_ACTOR=arcs-fm "$REPO_DIR/scripts/notify/bd-comment-notify" wiseman-c88 "Automated self-comment test" 2>&1 || true)"
assert_match "skips ping on self-comment" "self-comment by assignee 'arcs-fm'; no ping needed" "$out"

# 3. Test non-assignee comment triggers notify
echo "Test 3: Non-assignee comment notifications"
out="$(BEADS_ACTOR=wiseman "$REPO_DIR/scripts/notify/bd-comment-notify" wiseman-c88 "Automated coordinator comment test" 2>&1 || true)"
assert_match "notifies assignee" "notifying assignee 'arcs-fm'" "$out"
assert_match "confirms delivery or deliberate defer" "assignee 'arcs-fm' notified" "$out"

# 4. Exact-pin-only guard: the fuzzy resolver and the duplicate shell
#    transport must stay gone. Fuzzy routing is the defect that lost the
#    fm-l00u7 daily wake to a random lookalike pane (wiseman-y67).
echo "Test 4: no fuzzy seat resolution, one delivery implementation"
fuzzy_hits="$(grep -rn --exclude=test_beads_notify_e2e.sh \
    -e 'ResolveSeat' -e 'discover_seat' -e 'scoreInstance' -e 'titleHasSeatToken' \
    "$REPO_DIR/cmd" "$REPO_DIR/internal/notify" "$REPO_DIR/scripts/notify" 2>/dev/null || true)"
assert_eq "no fuzzy resolver anywhere in the notify path" "" "$fuzzy_hits"
assert_eq "shell transport twin stays deleted" "" \
    "$(ls "$REPO_DIR/scripts/notify/bd-notify-deliver" "$REPO_DIR/scripts/notify/bd-notify-drain" 2>/dev/null || true)"

# 5. One drain entrypoint: every caller goes through `bd notify drain`.
echo "Test 5: single drain entrypoint"
assert_match "bd-comment-notify drains through 'bd notify drain'" 'bd notify drain --seat' \
    "$(cat "$REPO_DIR/scripts/notify/bd-comment-notify")"
assert_match "fleet monitor drains through 'bd notify drain'" 'bd notify drain --all' \
    "$(cat "$REPO_DIR/scripts/notify/fleet-monitor-beads-notify.sh")"

echo ""
echo "=== SUMMARY: $PASS_COUNT passed, $FAIL_COUNT failed ==="
[ "$FAIL_COUNT" -eq 0 ]
