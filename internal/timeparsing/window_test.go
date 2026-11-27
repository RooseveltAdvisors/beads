package timeparsing

import (
	"testing"
	"time"
)

func TestParseWindow(t *testing.T) {
	w, err := ParseWindow("09:00-20:00 America/New_York")
	if err != nil {
		t.Fatalf("ParseWindow: %v", err)
	}
	if w.StartMin != 540 || w.EndMin != 1200 || w.Zone != "America/New_York" {
		t.Fatalf("ParseWindow = %#v, want 09:00-20:00 America/New_York", w)
	}
	if got := w.String(); got != "09:00-20:00 America/New_York" {
		t.Errorf("String() = %q, want the round trip", got)
	}
	zero, err := ParseWindow("")
	if err != nil || !zero.IsZero() {
		t.Errorf("ParseWindow(\"\") = %#v, %v; want the zero window", zero, err)
	}
	for _, bad := range []string{"09:00-20:00", "09:00 20:00 America/New_York", "9-20 America/New_York",
		"09:00-20:00 Not/AZone", "25:00-20:00 America/New_York", "09:60-20:00 America/New_York"} {
		if _, err := ParseWindow(bad); err == nil {
			t.Errorf("ParseWindow(%q) = nil error, want refusal", bad)
		}
	}
}

func TestQuietWindowContains(t *testing.T) {
	w, err := ParseWindow("09:00-20:00 America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// The window carries its zone: 13:00 UTC is 09:00 EDT in October, inside.
	inside := time.Date(2026, 10, 26, 13, 30, 0, 0, time.UTC)
	outside := time.Date(2026, 10, 26, 4, 30, 0, 0, time.UTC) // 00:30 EDT
	if !w.Contains(inside) {
		t.Errorf("Contains(%s) = false, want true (09:30 local)", inside)
	}
	if w.Contains(outside) {
		t.Errorf("Contains(%s) = true, want false (00:30 local)", outside)
	}
	// The start is inclusive, the end exclusive.
	if !w.Contains(time.Date(2026, 10, 26, 13, 0, 0, 0, time.UTC)) {
		t.Error("the window start must be inclusive")
	}
	if w.Contains(time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)) {
		t.Error("the window end must be exclusive")
	}
	// The zero window is always live.
	var zero QuietWindow
	if !zero.Contains(outside) {
		t.Error("the zero window must contain every instant")
	}
}

func TestQuietWindowWrapsMidnight(t *testing.T) {
	w, err := ParseWindow("22:00-06:00 America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if !w.Contains(time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC)) { // 19:00 EDT? no: 23:00 UTC = 19:00 EDT
		// 23:00 UTC on Oct 26 = 19:00 EDT: outside a 22:00-06:00 local window.
		if w.Contains(time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC)) {
			t.Error("19:00 local must be outside the 22:00-06:00 window")
		}
	}
	if !w.Contains(time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)) { // 23:00 EDT
		t.Error("23:00 local must be inside the wrapping window")
	}
	if !w.Contains(time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC)) { // 05:30 EDT
		t.Error("05:30 local must be inside the wrapping window")
	}
}

// TestNextOpen pins the quiet-reschedule target: the first live instant
// strictly after t, in the window's own zone, with the wrap and failure
// cases the sweep's wedge fix depends on.
func TestNextOpen(t *testing.T) {
	w, err := ParseWindow("09:00-20:00 America/New_York")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 03:30 UTC is 23:30 EDT the prior day: quiet, next open 09:00 EDT today.
	got, ok := w.NextOpen(time.Date(2026, 10, 26, 3, 30, 0, 0, time.UTC))
	if !ok {
		t.Fatal("quiet before open: want ok")
	}
	if want := time.Date(2026, 10, 26, 13, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("quiet 23:30 EDT: NextOpen = %v, want %v (09:00 EDT)", got, want)
	}
	// 21:00 UTC is 17:00 EDT: quiet-after-end waits for tomorrow's open.
	got, ok = w.NextOpen(time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("quiet after end: want ok")
	}
	if want := time.Date(2026, 10, 27, 13, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("quiet 17:00 EDT: NextOpen = %v, want %v (next day 09:00 EDT)", got, want)
	}
	// Wrap window 20:00-09:00: quiet midday reopens at 20:00 the same day.
	wrap, err := ParseWindow("20:00-09:00 America/New_York")
	if err != nil {
		t.Fatalf("parse wrap: %v", err)
	}
	got, ok = wrap.NextOpen(time.Date(2026, 10, 26, 16, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("wrap quiet: want ok")
	}
	if want := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("wrap quiet 12:00 EDT: NextOpen = %v, want %v (20:00 EDT)", got, want)
	}
	// Zero window and unusable zone answer false: the caller keeps due_at.
	if _, ok := (QuietWindow{}).NextOpen(time.Now()); ok {
		t.Error("zero window: NextOpen ok, want false")
	}
	if _, ok := (QuietWindow{StartMin: 540, EndMin: 1200, Zone: "Not/AZone"}).NextOpen(time.Now()); ok {
		t.Error("unusable zone: NextOpen ok, want false")
	}
}
