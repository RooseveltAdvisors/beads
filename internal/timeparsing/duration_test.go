package timeparsing

import (
	"testing"
	"time"
)

func TestParseCompactDuration(t *testing.T) {
	// Fixed reference time for deterministic tests
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		// Valid positive durations
		{
			name:  "+6h adds 6 hours",
			input: "+6h",
			want:  time.Date(2025, 6, 15, 18, 0, 0, 0, time.UTC),
		},
		{
			name:  "+1d adds 1 day",
			input: "+1d",
			want:  time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "+2w adds 2 weeks",
			input: "+2w",
			want:  time.Date(2025, 6, 29, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "+3m adds 3 months",
			input: "+3m",
			want:  time.Date(2025, 9, 15, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "+1y adds 1 year",
			input: "+1y",
			want:  time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		},

		// Valid negative durations (past)
		{
			name:  "-1d subtracts 1 day",
			input: "-1d",
			want:  time.Date(2025, 6, 14, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "-2w subtracts 2 weeks",
			input: "-2w",
			want:  time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "-6h subtracts 6 hours",
			input: "-6h",
			want:  time.Date(2025, 6, 15, 6, 0, 0, 0, time.UTC),
		},

		// No sign means positive
		{
			name:  "3m without sign adds 3 months",
			input: "3m",
			want:  time.Date(2025, 9, 15, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "1y without sign adds 1 year",
			input: "1y",
			want:  time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "6h without sign adds 6 hours",
			input: "6h",
			want:  time.Date(2025, 6, 15, 18, 0, 0, 0, time.UTC),
		},

		// Multi-digit amounts
		{
			name:  "+24h adds 24 hours",
			input: "+24h",
			want:  time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC),
		},
		{
			name:  "+365d adds 365 days",
			input: "+365d",
			want:  time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		},

		// Invalid inputs
		{
			name:    "6h+ (sign at end) is invalid",
			input:   "6h+",
			wantErr: true,
		},
		{
			name:    "++1d (double sign) is invalid",
			input:   "++1d",
			wantErr: true,
		},
		{
			name:    "1x (unknown unit) is invalid",
			input:   "1x",
			wantErr: true,
		},
		{
			name:    "empty string is invalid",
			input:   "",
			wantErr: true,
		},
		{
			name:    "just a number is invalid",
			input:   "6",
			wantErr: true,
		},
		{
			name:    "just a unit is invalid",
			input:   "h",
			wantErr: true,
		},
		{
			name:    "spaces are invalid",
			input:   "+ 6h",
			wantErr: true,
		},
		{
			name:    "ISO date is not compact duration",
			input:   "2025-01-15",
			wantErr: true,
		},
		{
			name:    "natural language is not compact duration",
			input:   "tomorrow",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCompactDuration(tt.input, now)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCompactDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && !got.Equal(tt.want) {
				t.Errorf("ParseCompactDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsCompactDuration(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"+6h", true},
		{"-1d", true},
		{"+2w", true},
		{"3m", true},
		{"1y", true},
		{"+24h", true},
		{"", false},
		{"tomorrow", false},
		{"2025-01-15", false},
		{"6h+", false},
		{"++1d", false},
		{"1x", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isCompactDuration(tt.input)
			if got != tt.want {
				t.Errorf("isCompactDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestParseCompactDuration_MonthBoundary tests month arithmetic edge cases.
func TestParseCompactDuration_MonthBoundary(t *testing.T) {
	// Jan 31 + 1 month = Feb 28 (or 29 in leap year)
	jan31 := time.Date(2025, 1, 31, 12, 0, 0, 0, time.UTC)
	got, err := ParseCompactDuration("+1m", jan31)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The step clamps to the last day of the shorter month rather than
	// overflowing into March the way time.AddDate does.
	want := time.Date(2025, 2, 28, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Jan 31 + 1m = %v, want %v", got, want)
	}
	leap, err := ParseCompactDuration("+1m", time.Date(2024, 1, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wantLeap := time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC); !leap.Equal(wantLeap) {
		t.Errorf("Jan 31 2024 + 1m = %v, want %v", leap, wantLeap)
	}
}

// TestParseCompactDuration_LeapYear tests leap year handling.
func TestParseCompactDuration_LeapYear(t *testing.T) {
	// Feb 28, 2024 (leap year) + 1d = Feb 29
	feb28_2024 := time.Date(2024, 2, 28, 12, 0, 0, 0, time.UTC)
	got, err := ParseCompactDuration("+1d", feb28_2024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Feb 28, 2024 + 1d = %v, want %v", got, want)
	}
}

// TestParseCompactDuration_PreservesTimezone tests that local timezone is preserved.
func TestParseCompactDuration_PreservesTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("timezone America/New_York not available")
	}

	now := time.Date(2025, 6, 15, 12, 0, 0, 0, loc)
	got, err := ParseCompactDuration("+1d", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Location() != loc {
		t.Errorf("timezone not preserved: got %v, want %v", got.Location(), loc)
	}
}

// TestParseCompactDuration_MinutesVsMonths pins the unit map that a real
// misread turned into a 2029 due date: a +30m due was taken as 30 minutes and
// parsed as 30 MONTHS (2026-10-16 + 30m = 2029-04-16). m is months; minutes
// spell out as min (or minute/minutes), and mo is the explicit month alias.
func TestParseCompactDuration_MinutesVsMonths(t *testing.T) {
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Time
	}{
		{"+30m", time.Date(2029, 4, 16, 12, 0, 0, 0, time.UTC)},     // MONTHS - the trap
		{"+30mo", time.Date(2029, 4, 16, 12, 0, 0, 0, time.UTC)},    // explicit months
		{"+30min", time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)}, // minutes
		{"+30mins", time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)},
		{"+30minute", time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)},
		{"+30minutes", time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)},
		{"+30MIN", time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)}, // case-insensitive
	}
	for _, c := range cases {
		got, err := ParseCompactDuration(c.in, base)
		if err != nil {
			t.Fatalf("ParseCompactDuration(%q): %v", c.in, err)
		}
		if !got.Equal(c.want) {
			t.Errorf("ParseCompactDuration(%q, %s) = %s, want %s", c.in, base.Format(time.RFC3339), got.Format(time.RFC3339), c.want.Format(time.RFC3339))
		}
	}
	// The old "minutes" spellings stay rejected - no silent guesses.
	for _, bad := range []string{"+30 m", "+30 minute s", "30min "} {
		if _, err := ParseCompactDuration(bad, base); err == nil {
			t.Errorf("ParseCompactDuration(%q) accepted; want an error", bad)
		}
	}
}
