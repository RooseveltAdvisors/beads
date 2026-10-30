package timeparsing

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// QuietWindow is the daily window in which a recurring bead is live. Outside
// it the bead is deliberately silent: the due sweep does not fire it,
// due_missed does not accrue, and the stale-claim and escalate checkers do not
// treat the silence as a zombie. The zone is explicit because the host's local
// zone is not the customer's.
//
// Format: "HH:MM-HH:MM IANA/Zone", for example "09:00-20:00 America/New_York".
// The start is inclusive, the end exclusive. A window whose end is not after
// its start wraps midnight; the common case does not wrap.
type QuietWindow struct {
	// StartMin and EndMin are minutes from local midnight in Zone.
	StartMin int
	EndMin   int
	// Zone is the IANA name the minutes are read in.
	Zone string
}

// ParseWindow parses the compact window syntax. An empty string yields the
// zero QuietWindow, which IsZero reports and which every caller treats as
// "no window: always live".
func ParseWindow(s string) (QuietWindow, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return QuietWindow{}, nil
	}
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return QuietWindow{}, fmt.Errorf("window %q: want \"HH:MM-HH:MM <IANA/Zone>\" (example: \"09:00-20:00 America/New_York\")", s)
	}
	span, zone := parts[0], parts[1]
	dash := strings.Index(span, "-")
	if dash < 0 {
		return QuietWindow{}, fmt.Errorf("window %q: want a HH:MM-HH:MM span", span)
	}
	start, err := parseWindowMinute(span[:dash])
	if err != nil {
		return QuietWindow{}, fmt.Errorf("window %q: %v", s, err)
	}
	end, err := parseWindowMinute(span[dash+1:])
	if err != nil {
		return QuietWindow{}, fmt.Errorf("window %q: %v", s, err)
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return QuietWindow{}, fmt.Errorf("window %q: unknown timezone %q", s, zone)
	}
	return QuietWindow{StartMin: start, EndMin: end, Zone: zone}, nil
}

// parseWindowMinute reads "HH:MM" as minutes from local midnight.
func parseWindowMinute(s string) (int, error) {
	colon := strings.Index(s, ":")
	if colon < 0 {
		return 0, fmt.Errorf("want HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(s[:colon])
	if err != nil {
		return 0, fmt.Errorf("want HH:MM, got %q", s)
	}
	m, err := strconv.Atoi(s[colon+1:])
	if err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("want HH:MM, got %q", s)
	}
	return h*60 + m, nil
}

// IsZero reports whether the window is unset: no window means always live.
func (w QuietWindow) IsZero() bool {
	return w.Zone == ""
}

// String renders the window back to its compact syntax.
func (w QuietWindow) String() string {
	if w.IsZero() {
		return ""
	}
	return fmt.Sprintf("%02d:%02d-%02d:%02d %s",
		w.StartMin/60, w.StartMin%60, w.EndMin/60, w.EndMin%60, w.Zone)
}

// Contains reports whether t falls inside the window. The zero window
// contains every instant: a bead with no window is always live.
func (w QuietWindow) Contains(t time.Time) bool {
	if w.IsZero() {
		return true
	}
	loc, err := time.LoadLocation(w.Zone)
	if err != nil {
		// A zone that stopped loading keeps the bead live rather than
		// silencing it forever.
		return true
	}
	minutes := t.In(loc).Hour()*60 + t.In(loc).Minute()
	if w.StartMin <= w.EndMin {
		return minutes >= w.StartMin && minutes < w.EndMin
	}
	// Wraps midnight: live from StartMin to 24:00 and 00:00 to EndMin.
	return minutes >= w.StartMin || minutes < w.EndMin
}

// NextOpen returns the first instant strictly after t at which the window
// is live again (Contains true). The bool is false for a zero window or a
// zone that no longer loads: the caller keeps due_at untouched, which is no
// worse than never having asked.
func (w QuietWindow) NextOpen(t time.Time) (time.Time, bool) {
	if w.IsZero() {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(w.Zone)
	if err != nil {
		return time.Time{}, false
	}
	lt := t.In(loc)
	midnight := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
	todayOpen := midnight.Add(time.Duration(w.StartMin) * time.Minute)
	if todayOpen.After(t) {
		return todayOpen, true
	}
	return todayOpen.AddDate(0, 0, 1), true
}
