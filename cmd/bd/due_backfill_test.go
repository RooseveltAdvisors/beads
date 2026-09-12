package main

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func backfillIssue(id string, created time.Time, mutate func(*types.Issue)) *types.Issue {
	issue := &types.Issue{
		ID:        id,
		Title:     "issue " + id,
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
		CreatedAt: created,
	}
	if mutate != nil {
		mutate(issue)
	}
	return issue
}

func day(d int) time.Time { return time.Date(2026, 3, d, 9, 0, 0, 0, time.UTC) }

// The plan is the whole contract of the dry run: what it counts is what --apply
// writes. This pins which beads are in scope and which are deliberately not.
func TestPlanDueBackfillSelectsOnlyUndatedWork(t *testing.T) {
	closedAt := day(2)
	issues := []*types.Issue{
		backfillIssue("bd-1", day(1), nil), // in scope
		backfillIssue("bd-2", day(3), nil), // in scope
		backfillIssue("bd-already", day(1), func(i *types.Issue) { // skipped: has a date
			due := day(20)
			i.DueAt = &due
		}),
		backfillIssue("bd-closed", day(1), func(i *types.Issue) { // skipped: no work left
			i.Status = types.StatusClosed
			i.ClosedAt = &closedAt
		}),
		backfillIssue("bd-event", day(1), func(i *types.Issue) { i.IssueType = types.TypeEvent }),
		backfillIssue("bd-wisp", day(1), func(i *types.Issue) { i.Ephemeral = true }),
		backfillIssue("bd-template", day(1), func(i *types.Issue) { i.IsTemplate = true }),
		backfillIssue("bd-federated", day(1), func(i *types.Issue) { i.SourceSystem = "github" }),
		nil, // a nil row must not panic the report
	}

	report := planDueBackfill(issues, 7*24*time.Hour)

	if report.Candidates != 2 {
		t.Fatalf("Candidates = %d, want 2 (rows: %+v)", report.Candidates, report.Rows)
	}
	if report.AlreadyDated != 1 {
		t.Errorf("AlreadyDated = %d, want 1", report.AlreadyDated)
	}
	if report.Scanned != len(issues) {
		t.Errorf("Scanned = %d, want %d", report.Scanned, len(issues))
	}
	if report.Applied {
		t.Error("planDueBackfill marked the report applied; the plan writes nothing")
	}
	if report.Updated != 0 {
		t.Errorf("Updated = %d, want 0 for a plan", report.Updated)
	}
	gotIDs := []string{report.Rows[0].ID, report.Rows[1].ID}
	if gotIDs[0] != "bd-1" || gotIDs[1] != "bd-2" {
		t.Errorf("rows = %v, want oldest-first [bd-1 bd-2]", gotIDs)
	}
}

// Each bead is dated from its OWN created_at, so the relative order of a
// backlog survives the backfill instead of collapsing onto one date.
func TestPlanDueBackfillDatesFromEachCreatedAt(t *testing.T) {
	interval := 7 * 24 * time.Hour
	issues := []*types.Issue{
		backfillIssue("bd-old", day(1), nil),
		backfillIssue("bd-new", day(10), nil),
	}
	report := planDueBackfill(issues, interval)
	for _, row := range report.Rows {
		if want := row.CreatedAt.Add(interval); !row.ProposedDue.Equal(want) {
			t.Errorf("%s proposed due = %v, want %v", row.ID, row.ProposedDue, want)
		}
	}
	if !report.Rows[0].ProposedDue.Before(report.Rows[1].ProposedDue) {
		t.Error("backfill collapsed the backlog's ordering")
	}
	if report.OldestDue == nil || !report.OldestDue.Equal(day(8)) {
		t.Errorf("OldestDue = %v, want %v", report.OldestDue, day(8))
	}
	if report.NewestDue == nil || !report.NewestDue.Equal(day(17)) {
		t.Errorf("NewestDue = %v, want %v", report.NewestDue, day(17))
	}
}

func TestPlanDueBackfillCountsByTypeAndStatus(t *testing.T) {
	issues := []*types.Issue{
		backfillIssue("bd-1", day(1), nil),
		backfillIssue("bd-2", day(2), func(i *types.Issue) { i.IssueType = types.TypeBug }),
		backfillIssue("bd-3", day(3), func(i *types.Issue) { i.Status = types.StatusInProgress }),
	}
	report := planDueBackfill(issues, 24*time.Hour)
	if report.ByType["task"] != 2 || report.ByType["bug"] != 1 {
		t.Errorf("ByType = %v, want task=2 bug=1", report.ByType)
	}
	if report.ByStatus["open"] != 2 || report.ByStatus["in_progress"] != 1 {
		t.Errorf("ByStatus = %v, want open=2 in_progress=1", report.ByStatus)
	}
}

func TestPlanDueBackfillEmptyRepository(t *testing.T) {
	report := planDueBackfill(nil, 24*time.Hour)
	if report.Candidates != 0 || len(report.Rows) != 0 || report.OldestDue != nil {
		t.Errorf("empty plan = %+v, want no candidates", report)
	}
}

func TestParseBackfillInterval(t *testing.T) {
	tests := []struct {
		raw  string
		want time.Duration
	}{
		{"+7d", 7 * 24 * time.Hour},
		{"+2w", 14 * 24 * time.Hour},
		{"+12h", 12 * time.Hour},
		{"3d", 3 * 24 * time.Hour},
	}
	for _, tc := range tests {
		got, err := parseBackfillInterval(tc.raw)
		if err != nil {
			t.Errorf("parseBackfillInterval(%q) error = %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseBackfillInterval(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
	// A cron expression is a schedule, not an offset: it cannot say "how far
	// past created_at", so it is refused rather than silently reinterpreted.
	for _, bad := range []string{"0 9 * * 1", "-7d", "next tuesday", "0d"} {
		if _, err := parseBackfillInterval(bad); err == nil {
			t.Errorf("parseBackfillInterval(%q) error = nil, want a refusal", bad)
		}
	}
}

func TestFormatCountMapIsStable(t *testing.T) {
	counts := map[string]int{"task": 3, "bug": 1, "chore": 2}
	want := "bug=1, chore=2, task=3"
	for i := 0; i < 5; i++ {
		if got := formatCountMap(counts); got != want {
			t.Fatalf("formatCountMap = %q, want %q", got, want)
		}
	}
	if got := formatCountMap(nil); got != "(none)" {
		t.Errorf("formatCountMap(nil) = %q, want (none)", got)
	}
}
