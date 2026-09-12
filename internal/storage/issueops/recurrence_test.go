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
			name: "recurring bead follows its own interval from now",
			cand: dueCandidate{id: "b", dueAt: at(2026, time.March, 1, 9), repeatPattern: "+1w"},
			want: now.AddDate(0, 0, 7),
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

func TestStatusPlaceholdersBindRatherThanInterpolate(t *testing.T) {
	list, args := statusPlaceholders([]types.Status{types.StatusOpen, types.StatusInProgress})
	if list != "?, ?" {
		t.Errorf("placeholder list = %q, want \"?, ?\"", list)
	}
	if len(args) != 2 || args[0] != "open" || args[1] != "in_progress" {
		t.Errorf("args = %v, want [open in_progress]", args)
	}
}
