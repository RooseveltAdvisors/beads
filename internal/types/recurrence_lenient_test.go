package types

import (
	"encoding/json"
	"testing"
)

func TestParseRecurrenceLenientTreatsUnformableRepeatAsInert(t *testing.T) {
	t.Parallel()
	for _, metadata := range []string{
		`{"repeat":3}`,
		`{"repeat":"weekly"}`,
		`{"repeat":"61 9 * * *","recurrence_start":"2026-09-08","recurrence_tz":"UTC"}`,
		`{"recurrence_start":"2026-09-08"}`,
	} {
		if got := ParseRecurrenceLenient(json.RawMessage(metadata), "jr-voice"); got != nil {
			t.Errorf("ParseRecurrenceLenient(%s) = %#v, want nil (inert legacy metadata)", metadata, got)
		}
	}

	got := ParseRecurrenceLenient(json.RawMessage(`{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York"}`), "jr-voice")
	if got == nil || got.Schedule != "daily" {
		t.Fatalf("ParseRecurrenceLenient() = %#v, want the parsed daily recurrence", got)
	}
}

func TestRecurrenceKeysEqual(t *testing.T) {
	t.Parallel()
	valid := `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York"}`
	tests := []struct {
		name        string
		before      string
		after       string
		wantCurrent bool
	}{
		{name: "identical recurrence keys", before: valid, after: valid, wantCurrent: true},
		{name: "sibling keys added or removed", before: valid, after: `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York","campaign":"blog"}`, wantCurrent: true},
		{name: "recurrence value edited", before: valid, after: `{"repeat":"weekly","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York"}`, wantCurrent: false},
		{name: "recurrence key added", before: `{}`, after: valid, wantCurrent: false},
		{name: "recurrence key removed", before: valid, after: `{}`, wantCurrent: false},
		{name: "non-string values compared semantically", before: `{"repeat":3}`, after: `{"repeat":3}`, wantCurrent: true},
		{name: "non-string values differ", before: `{"repeat":3}`, after: `{"repeat":4}`, wantCurrent: false},
		{name: "non-object metadata on both sides", before: `"legacy"`, after: `"legacy"`, wantCurrent: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RecurrenceKeysEqual(json.RawMessage(tc.before), json.RawMessage(tc.after)); got != tc.wantCurrent {
				t.Fatalf("RecurrenceKeysEqual() = %v, want %v", got, tc.wantCurrent)
			}
		})
	}
}
