package issueops

import (
	"testing"
	"time"
)

// TestCronEvaluatesInWindowZone is the regression for the 2026-11-05 firing
// gap: the cron "*/10 9-19" was evaluated in UTC while the quiet window read
// America/New_York, leaving a 5h daily gap where the window was open but the
// cron was silent. The walk inputs must be in the window's zone.
func TestCronEvaluatesInWindowZone(t *testing.T) {
	window := "09:00-20:00 America/New_York"
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone unavailable: %v", err)
	}
	// 21:00 UTC = 16:00 EST: inside the window (9-20 ET), inside cron
	// hours 9-19 ET, but OUTSIDE cron hours 9-19 UTC - the daily gap
	// where UTC evaluation went silent while the window stayed open.
	now := time.Date(2026, 11, 5, 21, 0, 0, 0, time.UTC)
	due := now.Add(-10 * time.Minute) // due 20:50 UTC = 15:50 EST, overdue
	c := dueCandidate{
		id:            "tz-test",
		dueAt:         due,
		repeatPattern: "*/10 9-19 * * *",
		repeatWindow:  window,
	}
	next, ok := nextDueAfterMissInWindow(c, now)
	if !ok {
		t.Fatal("want an occurrence, series cannot supply one")
	}
	// The next occurrence must be within today's ET hours 9-19, i.e. the
	// window is open at next. In UTC: 14:00-23:59 for Nov 5.
	nextET := next.In(loc)
	if nextET.Hour() < 9 || nextET.Hour() > 19 {
		t.Errorf("next = %v (ET %v), want an hour in 9-19 ET", next, nextET)
	}
	if nextET.Day() != 5 {
		t.Errorf("next = %v (ET %v), want same day", next, nextET)
	}
	// And next must be strictly after now.
	if !next.After(now) {
		t.Errorf("next = %v, want after now %v", next, now)
	}
}
