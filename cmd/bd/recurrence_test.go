package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

func recurrenceTestCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	registerCommonIssueFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestApplyCreateRecurrenceFlagsDailyBlog(t *testing.T) {
	t.Parallel()
	cmd := recurrenceTestCommand(t,
		"--assignee", "jr-voice",
		"--repeat", "0 9 * * *",
		"--recurrence-start", "2026-09-08",
		"--recurrence-tz", "America/New_York",
	)
	metadata, err := applyCreateRecurrenceFlags(cmd, json.RawMessage(`{"campaign":"jonroosevelt.com"}`), "jr-voice")
	if err != nil {
		t.Fatal(err)
	}
	recurrence, err := types.ParseRecurrence(metadata, "jr-voice")
	if err != nil {
		t.Fatal(err)
	}
	if recurrence.Schedule != "0 9 * * *" || recurrence.Start != "2026-09-08" || recurrence.End != "" || recurrence.Timezone != "America/New_York" {
		t.Fatalf("unexpected recurrence: %#v", recurrence)
	}
	if !strings.Contains(string(metadata), `"campaign":"jonroosevelt.com"`) {
		t.Fatalf("unrelated metadata was not preserved: %s", metadata)
	}
}

func TestRecurrenceMetadataEditsClearDefinition(t *testing.T) {
	t.Parallel()
	cmd := recurrenceTestCommand(t, "--repeat=")
	set, unset, err := recurrenceMetadataEdits(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 0 || len(unset) != 4 {
		t.Fatalf("set=%v unset=%v, want complete recurrence removal", set, unset)
	}
}

func TestRecurrenceMetadataEditsClearAllConflictsWithSetFlag(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--recurrence-start=2026-10-01", "--recurrence-end=2026-12-31", "--recurrence-tz=UTC"} {
		cmd := recurrenceTestCommand(t, "--repeat=", flag)
		_, _, err := recurrenceMetadataEdits(cmd)
		if err == nil || !strings.Contains(err.Error(), "clears the whole recurrence contract") {
			t.Fatalf("recurrenceMetadataEdits(--repeat=, %s) error = %v, want clear-all conflict refusal", flag, err)
		}
	}
}

func TestFormatIssueMetadataShowsRecurrence(t *testing.T) {
	t.Parallel()
	issue := &types.Issue{
		Assignee:  "jr-voice",
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"repeat":"0 9 * * *","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York"}`),
	}
	out := formatIssueMetadata(issue)
	for _, want := range []string{"Assignee: jr-voice", "Repeat: 0 9 * * *", "Start: 2026-09-08", "Timezone: America/New_York"} {
		if !strings.Contains(out, want) {
			t.Errorf("formatIssueMetadata() missing %q:\n%s", want, out)
		}
	}
}
