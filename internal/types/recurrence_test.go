package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseRecurrence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		metadata string
		assignee string
		wantNil  bool
		wantErr  string
	}{
		{name: "one off", metadata: `{}`, wantNil: true},
		{name: "unrelated scalar metadata", metadata: `"legacy"`, wantNil: true},
		{name: "daily", metadata: `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York"}`, assignee: "jr-voice"},
		{name: "weekly with end", metadata: `{"repeat":"weekly","recurrence_start":"2026-09-08T09:00","recurrence_end":"2026-12-31T17:00","recurrence_tz":"America/New_York"}`, assignee: "jr-voice"},
		{name: "cron", metadata: `{"repeat":"0 9 * * *","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York"}`, assignee: "jr-voice"},
		{name: "malformed schedule", metadata: `{"repeat":"61 9 * * *","recurrence_start":"2026-09-08","recurrence_tz":"UTC"}`, assignee: "jr-voice", wantErr: "invalid recurrence schedule"},
		{name: "unknown timezone", metadata: `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"Mars/Olympus"}`, assignee: "jr-voice", wantErr: "invalid recurrence timezone"},
		{name: "implicit local timezone", metadata: `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"Local"}`, assignee: "jr-voice", wantErr: "explicit IANA timezone"},
		{name: "end before start", metadata: `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_end":"2026-09-07","recurrence_tz":"UTC"}`, assignee: "jr-voice", wantErr: "is before recurrence_start"},
		{name: "end equal to the start instant", metadata: `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_end":"2026-09-08T00:00","recurrence_tz":"UTC"}`, assignee: "jr-voice"},
		{name: "same-day date-only end after datetime start", metadata: `{"repeat":"daily","recurrence_start":"2026-09-08T09:00","recurrence_end":"2026-09-08","recurrence_tz":"America/New_York"}`, assignee: "jr-voice"},
		{name: "date-only end before datetime start", metadata: `{"repeat":"daily","recurrence_start":"2026-09-08T09:00","recurrence_end":"2026-09-07","recurrence_tz":"America/New_York"}`, assignee: "jr-voice", wantErr: "is before recurrence_start"},
		{name: "missing owner", metadata: `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"UTC"}`, wantErr: "canonical owning agent"},
		{name: "orphan bound", metadata: `{"recurrence_start":"2026-09-08"}`, assignee: "jr-voice", wantErr: `requires "repeat"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRecurrence(json.RawMessage(tc.metadata), tc.assignee)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseRecurrence() error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRecurrence() error = %v", err)
			}
			if (got == nil) != tc.wantNil {
				t.Fatalf("ParseRecurrence() = %#v, want nil=%v", got, tc.wantNil)
			}
		})
	}
}
