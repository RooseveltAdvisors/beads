package issueops

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/types"
)

// TestQuietSweepReschedulesToWindowEdge is the regression for the wake-clock
// wedge: a recurring bead past due inside its quiet window used to be skipped
// with due_at left in the past, and bd-due-wake - which sleeps until the
// earliest due_at - re-swept at its sleep floor for hours (observed live
// 2026-10-30: fm-mw4xg, due_at 09:00Z, window 09:00-20:00 America/New_York,
// clock looping until 13:00Z). The skip must move due_at to the window's
// next opening edge WITHOUT counting a miss or enqueueing a fire.
func TestQuietSweepReschedulesToWindowEdge(t *testing.T) {
	// A window live one minute per day is quiet at (almost) any test-run
	// hour, so the regression holds around the clock.
	const window = "00:01-00:02 UTC"
	w, err := timeparsing.ParseWindow(window)
	if err != nil {
		t.Fatalf("parse window: %v", err)
	}
	now := time.Now().UTC()
	if w.Contains(now) {
		t.Skip("test ran inside the one live minute; rerun")
	}
	open, ok := w.NextOpen(now)
	if !ok || !open.After(now) {
		t.Fatalf("NextOpen = %v ok=%v, want a future instant", open, ok)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	past := now.Add(-2 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("FROM issues")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "due_at", "repeat_pattern", "repeat_start", "repeat_end", "repeat_window", "due_missed", "priority"}).
			AddRow("fm-quiet", past, "*/10 * * * *", nil, nil, window, 96, 2))
	// The wedge fix: the quiet skip EXECUTES a reschedule whose SET clause
	// touches due_at, updated_at and row_lock only - no due_missed, no
	// priority, no fire.
	mock.ExpectExec(regexp.QuoteMeta("SET due_at = ?, updated_at = ?, row_lock = ?")).
		WithArgs(open, sqlmock.AnyArg(), sqlmock.AnyArg(), "fm-quiet", past, "open", "in_progress").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if _, _, err := sweepDueBeadsInTable(context.Background(), tx, "issues", "events", []types.Status{types.StatusOpen, types.StatusInProgress}); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
