package issueops

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func at(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.UTC)
}

// A missed deadline must not stay in the past: it is pushed forward so it can
// fire again, which is both the nag and the sweep's idempotency mechanism.
func TestNextDueAfterMissRescheduling(t *testing.T) {
	now := at(2026, time.March, 10, 12) // a Tuesday
	tests := []struct {
		name string
		cand dueCandidate
		want time.Time
	}{
		{
			name: "non-recurring bead gets one grace period",
			cand: dueCandidate{id: "a", dueAt: at(2026, time.March, 1, 9)},
			want: now.Add(DueMissGrace),
		},
		{
			name: "recurring interval bead steps from its own due date, not from now",
			cand: dueCandidate{id: "b", dueAt: at(2026, time.March, 1, 9), repeatPattern: "+1w"},
			want: at(2026, time.March, 15, 9), // Mar 1 -> Mar 8 (still past) -> Mar 15
		},
		{
			name: "recurring cron bead lands on its next occurrence, not the next missed one",
			cand: dueCandidate{id: "c", dueAt: at(2026, time.January, 5, 9), repeatPattern: "0 9 * * 1"},
			want: at(2026, time.March, 16, 9), // the Monday after now
		},
		{
			name: "exhausted series falls back to the grace period so it still nags",
			cand: dueCandidate{
				id: "d", dueAt: at(2026, time.March, 1, 9), repeatPattern: "+1w",
				repeatEnd: ptr(at(2026, time.February, 1, 9)),
			},
			want: now.Add(DueMissGrace),
		},
		{
			name: "unparseable pattern falls back rather than leaving the date in the past",
			cand: dueCandidate{id: "e", dueAt: at(2026, time.March, 1, 9), repeatPattern: "every tuesday"},
			want: now.Add(DueMissGrace),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nextDueAfterMiss(tc.cand, now)
			if !got.Equal(tc.want) {
				t.Errorf("nextDueAfterMiss = %v, want %v", got, tc.want)
			}
			if !got.After(tc.cand.dueAt) {
				t.Errorf("nextDueAfterMiss = %v did not advance past the missed date %v", got, tc.cand.dueAt)
			}
		})
	}
}

// The successor carries the WORK forward and leaves this instance's own state
// behind: nothing about the closed instance's completion may leak into it.
func TestNextRecurrenceInstanceCarriesWorkNotInstanceState(t *testing.T) {
	closedAt := at(2026, time.March, 10, 17)
	prev := &types.Issue{
		ID:                 "bd-prev",
		Title:              "Weekly report",
		Description:        "desc",
		Design:             "design",
		AcceptanceCriteria: "criteria",
		Notes:              "notes",
		Status:             types.StatusClosed,
		Priority:           1,
		IssueType:          types.TypeChore,
		Assignee:           "worker",
		Owner:              "owner@example.com",
		ClosedAt:           &closedAt,
		CloseReason:        "done",
		ClosedBySession:    "session-1",
		SpecID:             "spec-1",
		ExternalRef:        strPtr("gh-42"),
		CompactionLevel:    3,
		RepeatPattern:      "+1w",
		Labels:             []string{"ops"},
	}
	due := at(2026, time.March, 17, 9)

	next := nextRecurrenceInstance(prev, due, "closer")

	if next.Title != prev.Title || next.Description != prev.Description || next.Priority != prev.Priority ||
		next.IssueType != prev.IssueType || next.RepeatPattern != prev.RepeatPattern {
		t.Errorf("successor lost the work definition: %+v", next)
	}
	if next.Assignee != "worker" || next.Owner != "owner@example.com" {
		t.Errorf("successor lost ownership: assignee=%q owner=%q", next.Assignee, next.Owner)
	}
	if len(next.Labels) != 1 || next.Labels[0] != "ops" {
		t.Errorf("successor labels = %v, want [ops]", next.Labels)
	}
	if next.Status != types.StatusOpen {
		t.Errorf("successor status = %q, want open", next.Status)
	}
	if next.DueAt == nil || !next.DueAt.Equal(due) {
		t.Errorf("successor due = %v, want %v", next.DueAt, due)
	}
	if next.DueSource != types.DueSourceRepeat {
		t.Errorf("successor due_source = %q, want %q", next.DueSource, types.DueSourceRepeat)
	}
	// Instance-specific state must NOT ride along.
	if next.ID != "" || next.ClosedAt != nil || next.CloseReason != "" || next.ClosedBySession != "" {
		t.Errorf("successor inherited closure state: %+v", next)
	}
	if next.ExternalRef != nil || next.SpecID != "" || next.CompactionLevel != 0 {
		t.Errorf("successor inherited instance identity: ref=%v spec=%q compaction=%d",
			next.ExternalRef, next.SpecID, next.CompactionLevel)
	}
	if next.CreatedBy != "closer" {
		t.Errorf("successor created_by = %q, want the closing actor", next.CreatedBy)
	}
	// Mutating the successor's labels must not reach back into the original.
	next.Labels[0] = "changed"
	if prev.Labels[0] != "ops" {
		t.Error("successor aliases the previous instance's label slice")
	}
}

func strPtr(s string) *string { return &s }

// A successor's due date comes from the series, so closing early or late does
// not drag the schedule with it. This pins the choice of anchor.
func TestNextOccurrenceAnchorsOnDueDateNotCloseTime(t *testing.T) {
	due := at(2026, time.March, 16, 9)
	issue := &types.Issue{RepeatPattern: "+1w", DueAt: &due}
	next, ok, err := issue.NextOccurrence(due)
	if err != nil || !ok {
		t.Fatalf("NextOccurrence = (%v, %v, %v)", next, ok, err)
	}
	if want := due.AddDate(0, 0, 7); !next.Equal(want) {
		t.Errorf("NextOccurrence = %v, want %v", next, want)
	}
}

func TestScheduledSweepResultCommitMessage(t *testing.T) {
	tests := []struct {
		name   string
		result ScheduledSweepResult
		want   string
	}{
		{"defers only", ScheduledSweepResult{Defers: WakeDefersResult{Issues: []string{"a", "b"}}},
			"bd: wake 2 expired defer(s)"},
		{"due only", ScheduledSweepResult{Due: DueSweepResult{Issues: []string{"a"}}},
			"bd: trigger 1 due bead(s)"},
		{"both", ScheduledSweepResult{
			Defers: WakeDefersResult{Issues: []string{"a"}},
			Due:    DueSweepResult{Issues: []string{"b", "c"}},
		}, "bd: wake 1 expired defer(s), trigger 2 due bead(s)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.result.CommitMessage(); got != tc.want {
				t.Errorf("CommitMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScheduledSweepResultCounts(t *testing.T) {
	result := ScheduledSweepResult{
		Defers: WakeDefersResult{Issues: []string{"a"}, Wisps: []string{"w1", "w2"}},
		Due:    DueSweepResult{Issues: []string{"b", "c"}, Wisps: []string{"w3"}},
	}
	if got := result.IssueRows(); got != 3 {
		t.Errorf("IssueRows() = %d, want 3", got)
	}
	if got := result.WispRows(); got != 3 {
		t.Errorf("WispRows() = %d, want 3", got)
	}
}

// The sweep fires on the workspace's own ACTIVE vocabulary, not on a
// not-closed exclusion: a bead parked in a custom DONE status must not fire a
// deadline forever, and one in a custom ACTIVE status must not be invisible to
// the sweep.
func TestDueSweepLiveStatusesFollowTheWorkspaceVocabulary(t *testing.T) {
	live := dueSweepLiveStatuses([]types.CustomStatus{
		{Name: "shipped", Category: types.CategoryDone},
		{Name: "reviewing", Category: types.CategoryActive},
	})
	got := map[types.Status]bool{}
	for _, status := range live {
		got[status] = true
	}
	if !got["reviewing"] {
		t.Error("a custom ACTIVE status is invisible to the due sweep")
	}
	if got["shipped"] {
		t.Error("a custom DONE status would fire a deadline forever")
	}
	if got[types.StatusClosed] {
		t.Error("closed beads would fire a deadline forever")
	}
	// A deferred bead is hidden until its own date; the defer wake — which runs
	// first, in the same transaction — is what returns it to open.
	if got[types.StatusDeferred] {
		t.Error("a deferred bead fired before its defer date arrived")
	}
	for _, want := range []types.Status{types.StatusOpen, types.StatusInProgress, types.StatusBlocked} {
		if !got[want] {
			t.Errorf("status %q is invisible to the due sweep", want)
		}
	}
}

// A weekly bead missed by several weeks must land on its own weekday and time:
// rescheduling from the sweep's clock would drag the whole series to whenever
// the sweep happened to run.
func TestNextDueAfterMissKeepsTheIntervalAnchor(t *testing.T) {
	dueAt := at(2026, time.March, 2, 9) // Monday 09:00
	now := at(2026, time.March, 25, 15) // Wednesday 15:00, three weeks later
	got := nextDueAfterMiss(dueCandidate{id: "a", dueAt: dueAt, repeatPattern: "+1w"}, now)
	want := at(2026, time.March, 30, 9) // the next Monday 09:00
	if !got.Equal(want) {
		t.Fatalf("nextDueAfterMiss = %v, want %v", got, want)
	}
	if got.Weekday() != dueAt.Weekday() || got.Hour() != dueAt.Hour() {
		t.Errorf("rescheduled to %v, which lost the %s %02d:00 anchor", got, dueAt.Weekday(), dueAt.Hour())
	}
	// A cron rule already expresses its anchor, so it matches from now.
	cron := nextDueAfterMiss(dueCandidate{id: "c", dueAt: dueAt, repeatPattern: "0 9 * * 1"}, now)
	if !cron.Equal(want) {
		t.Errorf("cron reschedule = %v, want %v", cron, want)
	}
	// A miss older than the horizon cannot be walked and falls back to grace.
	ancient := nextDueAfterMiss(dueCandidate{id: "d", dueAt: at(2015, time.March, 2, 9), repeatPattern: "+1w"}, now)
	if !ancient.Equal(now.Add(DueMissGrace)) {
		t.Errorf("ancient miss = %v, want the grace fallback %v", ancient, now.Add(DueMissGrace))
	}
}

// The update funnel validates the triple it would LAND, not the one field it
// was handed, and stopping a series clears its bounds along with the pattern.
func TestValidateRecurrenceUpdateChecksTheMergedTriple(t *testing.T) {
	start := at(2026, time.January, 1, 0)
	end := at(2027, time.January, 1, 0)
	recurring := &types.Issue{RepeatPattern: "+1w", RepeatStart: &start, RepeatEnd: &end}
	plain := &types.Issue{}

	tests := []struct {
		name    string
		old     *types.Issue
		updates map[string]interface{}
		wantErr bool
	}{
		{"no recurrence keys is not checked", plain, map[string]interface{}{"title": "x"}, false},
		{"a bound on a non-recurring bead is refused", plain, map[string]interface{}{"repeat_end": end}, true},
		{"a bound with a pattern in the same update is accepted", plain, map[string]interface{}{"repeat_pattern": "+1d", "repeat_end": end}, false},
		{"an end before the stored start is refused", recurring, map[string]interface{}{"repeat_end": at(2025, time.June, 1, 0)}, true},
		{"a start after the stored end is refused", recurring, map[string]interface{}{"repeat_start": &end, "repeat_end": &start}, true},
		{"clearing the pattern alone would strand the stored bounds", recurring, map[string]interface{}{"repeat_pattern": ""}, true},
		{"clearing a bound to nil is accepted", recurring, map[string]interface{}{"repeat_end": nil}, false},
		{"a non-time bound value is refused", recurring, map[string]interface{}{"repeat_end": "tomorrow"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRecurrenceUpdate(tc.old, tc.updates)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateRecurrenceUpdate = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}

	// The funnel clears the bounds BEFORE validating, so stopping a bounded
	// series is a legal single update.
	updates := map[string]interface{}{"repeat_pattern": ""}
	ClearRecurrenceBoundsOnStop(updates)
	if v, ok := updates["repeat_start"]; !ok || v != nil {
		t.Errorf("repeat_start = %v (present %v), want an explicit nil", v, ok)
	}
	if v, ok := updates["repeat_end"]; !ok || v != nil {
		t.Errorf("repeat_end = %v (present %v), want an explicit nil", v, ok)
	}
	if err := ValidateRecurrenceUpdate(recurring, updates); err != nil {
		t.Errorf("stopping a bounded series must validate once the bounds are cleared, got %v", err)
	}
	keep := map[string]interface{}{"repeat_pattern": "+2w"}
	ClearRecurrenceBoundsOnStop(keep)
	if _, ok := keep["repeat_start"]; ok {
		t.Error("changing the pattern must not touch the bounds")
	}
}

func TestStatusPlaceholdersBindRatherThanInterpolate(t *testing.T) {
	list, args := statusPlaceholders([]types.Status{types.StatusOpen, types.StatusInProgress})
	if list != "?, ?" {
		t.Errorf("placeholder list = %q, want \"?, ?\"", list)
	}
	if len(args) != 2 || args[0] != "open" || args[1] != "in_progress" {
		t.Errorf("args = %v, want [open in_progress]", args)
	}
}
