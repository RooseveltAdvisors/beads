package issueops

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// The captain's spec (2026-10-27): a live customer thread runs every 10
// minutes inside 09:00-20:00 America/New_York and stops entirely outside it.
// The cron grid gives the schedule; these cases pin that the NAG CHECKERS
// honor the quiet window instead of waking the owner every morning.
func TestRepeatWindowQuiet(t *testing.T) {
	in := &types.Issue{RepeatWindow: "09:00-20:00 America/New_York"}
	// 13:30 UTC is 09:30 EDT: live.
	if RepeatWindowQuiet(in, time.Date(2026, 10, 26, 13, 30, 0, 0, time.UTC)) {
		t.Error("09:30 local is inside the window: not quiet")
	}
	// 03:30 UTC is 23:30 EDT the previous day: quiet by design.
	if !RepeatWindowQuiet(in, time.Date(2026, 10, 26, 3, 30, 0, 0, time.UTC)) {
		t.Error("23:30 local is outside the window: quiet")
	}
	// No window, no quiet.
	none := &types.Issue{}
	if RepeatWindowQuiet(none, time.Date(2026, 10, 26, 3, 30, 0, 0, time.UTC)) {
		t.Error("a bead with no window is never quiet")
	}
	// A garbage window keeps the bead live rather than silencing it.
	garbage := &types.Issue{RepeatWindow: "not a window"}
	if RepeatWindowQuiet(garbage, time.Date(2026, 10, 26, 3, 30, 0, 0, time.UTC)) {
		t.Error("an unparseable window must not silence a bead")
	}
}
