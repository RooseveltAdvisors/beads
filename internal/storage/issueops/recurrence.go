package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage/dberrors"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/workapi"
)

// DueTriggerActor is the actor recorded on a due-date event. A constant rather
// than the invoking session's actor, for DeferWakeActor's reason: the trigger
// is the system honoring a deadline, not something the reader who happened to
// run the sweep did.
const DueTriggerActor = "bd-due-trigger"

// RecurrenceSpawnActor is the actor recorded on a successor bead a close
// spawned, when the closer's own actor is unavailable.
const RecurrenceSpawnActor = "bd-recurrence"

// DueMissGrace is how far forward the sweep pushes a NON-recurring bead's due
// date once its deadline has passed with the work still open.
//
// A due date that stays in the past fires once and is then indistinguishable
// from every other overdue bead — nothing escalates a second time, which is
// exactly the failure a deadline exists to prevent. Rescheduling turns a missed
// date into a repeating nudge, and doubles as the sweep's idempotency
// mechanism: the advanced date is what stops the same bead re-firing on the
// next read. A recurring bead uses its own pattern instead.
const DueMissGrace = 24 * time.Hour

// DueSweepResult reports what one sweep fired, per table. Issues are
// permanent-table ids and are the only ones that decide whether the caller
// mints a Dolt commit; wisp tables are dolt_ignored.
type DueSweepResult struct {
	Issues []string
	Wisps  []string
}

// DueSweepCommitMessage names a sweep's dolt commit. n is the number of
// permanent issues that fired; callers with n == 0 should not commit at all.
func DueSweepCommitMessage(n int) string {
	return fmt.Sprintf("bd: trigger %d due bead(s)", n)
}

// SweepDueBeadsInTx fires every open bead whose due date has arrived.
//
// For each one it records a types.EventDue audit event — the rail an external
// watcher consumes, the same one `bd events` and the events journal already
// carry — and then moves the due date forward so the bead is not re-fired on
// the next read. Forward means the next occurrence of its repeat pattern for a
// recurring bead, and one DueMissGrace for everything else; a recurring bead
// whose series has run out falls back to the grace interval, so a deadline
// still nags rather than going quiet.
//
// It is the exact shape of WakeExpiredDefersInTx and shares its contract: the
// caller owns Dolt versioning (commit iff len(result.Issues) > 0) and must
// treat a sweep failure as advisory — a ready listing never fails because the
// sweep could not run. The snapshot-then-recheck structure means a bead closed
// or rescheduled between the SELECT and its UPDATE is simply skipped.
func SweepDueBeadsInTx(ctx context.Context, tx DBTX) (DueSweepResult, error) {
	var result DueSweepResult
	live, err := dueSweepLiveStatusesInTx(ctx, tx)
	if err != nil {
		return result, err
	}
	issues, err := sweepDueBeadsInTable(ctx, tx, "issues", "events", live)
	if err != nil {
		return result, err
	}
	result.Issues = issues
	wisps, err := sweepDueBeadsInTable(ctx, tx, "wisps", "wisp_events", live)
	if err != nil {
		if dberrors.IsTableNotExist(err) {
			return result, nil
		}
		return result, err
	}
	result.Wisps = wisps
	return result, nil
}

// dueSweepLiveStatusesInTx lists the statuses a bead can be in and still come
// due: work that is neither finished nor deliberately hidden.
//
// It is an explicit LIST built from the workspace's own vocabulary rather than
// a `NOT IN ('closed', 'deferred')` exclusion, for the reason
// workapi.NotDoneStatusesForSweep records: a workspace can configure custom
// statuses, and an exclusion would fire a bead parked in a custom DONE status
// forever while never reaching one in a custom ACTIVE status. Reading the
// vocabulary is required, not best-effort — guessing it wrong shows up as an
// event storm on finished work.
//
// deferred is excluded even though it is not a done category: a deferred bead
// is hidden until its own date by contract, and WakeExpiredDefersInTx — which
// runs first in the same transaction — is what returns it to open. Once woken
// it is `open` and this sweep sees it in the same pass.
func dueSweepLiveStatusesInTx(ctx context.Context, tx DBTX) ([]types.Status, error) {
	custom, err := ResolveCustomStatusesDetailedInTx(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("due sweep: resolve custom statuses: %w", err)
	}
	return dueSweepLiveStatuses(custom), nil
}

// dueSweepLiveStatuses is the vocabulary decision itself, split from the read
// above so it can be checked without a database.
func dueSweepLiveStatuses(custom []types.CustomStatus) []types.Status {
	var live []types.Status
	for _, status := range workapi.NotDoneStatusesForSweep(custom) {
		if status == types.StatusDeferred {
			continue
		}
		live = append(live, status)
	}
	return live
}

// dueCandidate is one row the sweep picked up, carrying just the fields needed
// to compute its next due date.
type dueCandidate struct {
	id            string
	dueAt         time.Time
	repeatPattern string
	repeatStart   *time.Time
	repeatEnd     *time.Time
}

// the status placeholders are generated, never interpolated values.
//
//nolint:gosec // G201: table and eventsTable are hardcoded constants from the caller;
func sweepDueBeadsInTable(ctx context.Context, tx DBTX, table, eventsTable string, live []types.Status) ([]string, error) {
	if len(live) == 0 {
		return nil, nil
	}
	statusList, statusArgs := statusPlaceholders(live)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, due_at, repeat_pattern, repeat_start, repeat_end
		FROM %s
		WHERE due_at IS NOT NULL AND due_at <= UTC_TIMESTAMP()
		  AND status IN (%s)
	`, table, statusList), statusArgs...)
	if err != nil {
		return nil, fmt.Errorf("due sweep: scan %s: %w", table, err)
	}
	var candidates []dueCandidate
	for rows.Next() {
		var c dueCandidate
		var pattern sql.NullString
		var start, end sql.NullTime
		if err := rows.Scan(&c.id, &c.dueAt, &pattern, &start, &end); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("due sweep: scan %s row: %w", table, err)
		}
		c.repeatPattern = pattern.String
		if start.Valid {
			c.repeatStart = &start.Time
		}
		if end.Valid {
			c.repeatEnd = &end.Time
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("due sweep: iterate %s: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("due sweep: close %s rows: %w", table, err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	var fired []string
	now := time.Now().UTC()
	for _, c := range candidates {
		next := nextDueAfterMiss(c, now)
		// The predicate is repeated so a bead closed, deferred, or rescheduled
		// between the SELECT and here matches nothing and is left alone rather
		// than clobbered.
		args := append([]any{next, string(types.DueSourceRepeat), now, freshRowLock(), c.id, c.dueAt}, statusArgs...)
		res, err := tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE %s
			SET due_at = ?, due_source = ?, updated_at = ?, row_lock = ?
			WHERE id = ? AND due_at = ? AND status IN (%s)
		`, table, statusList), args...)
		if err != nil {
			return fired, fmt.Errorf("due sweep reschedule %s: %w", c.id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fired, fmt.Errorf("due sweep reschedule %s rows affected: %w", c.id, err)
		}
		if n == 0 {
			continue // rescued concurrently — leave it be
		}
		if err := RecordFullEventInTable(ctx, tx, eventsTable, c.id, types.EventDue,
			DueTriggerActor, c.dueAt.UTC().Format(time.RFC3339), next.Format(time.RFC3339)); err != nil {
			return fired, fmt.Errorf("record due event for %s: %w", c.id, err)
		}
		// A reschedule is a field change, so it journals as an update — past
		// the rows-affected recheck, so a concurrently-rescued bead records
		// nothing.
		if err := RecordEventInTx(ctx, tx, EventUpdate, c.id, DueTriggerActor); err != nil {
			return fired, err
		}
		fired = append(fired, c.id)
	}
	return fired, nil
}

// statusPlaceholders renders a status set as a bound-parameter list, so the
// vocabulary reaches SQL as arguments rather than interpolated text.
func statusPlaceholders(statuses []types.Status) (string, []any) {
	marks := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, status := range statuses {
		marks[i] = "?"
		args[i] = string(status)
	}
	return strings.Join(marks, ", "), args
}

// nextDueAfterMiss computes where a fired bead's due date moves to. A recurring
// bead follows its own pattern; everything else — including a recurring bead
// whose series has ended or whose pattern no longer parses — gets one
// DueMissGrace, so a deadline keeps nagging instead of going silent.
func nextDueAfterMiss(c dueCandidate, now time.Time) time.Time {
	if c.repeatPattern != "" {
		probe := &types.Issue{
			RepeatPattern: c.repeatPattern,
			RepeatStart:   c.repeatStart,
			RepeatEnd:     c.repeatEnd,
		}
		// Advance from NOW, not from the missed date: a weekly bead left
		// unattended for a month should land on the next Monday, not walk
		// forward one occurrence per sweep until it catches up.
		if next, ok, err := probe.NextOccurrence(now); err == nil && ok {
			return next
		}
	}
	return now.Add(DueMissGrace)
}

// SpawnResult reports what a recurrence spawn created. ID is "" when nothing
// was spawned, and ChangedTables is then empty.
//
// ChangedTables exists because a spawn writes MORE than the issues row: the
// successor's labels live in their own table, and a close that stages only
// {issues, events} would leave them dirty-but-uncommitted in the working set —
// present locally, absent from every clone. Callers that stage a fixed table
// list must union this in. See RecurrenceSpawnTables for the closed set.
type SpawnResult struct {
	ID            string
	ChangedTables map[string]bool
}

// RecurrenceSpawnTables is every table a recurrence spawn can write. It is a
// fixed set — the successor is one issues row, its audit events, and its
// labels — so a close path that stages a static list can simply include it.
// Staging a table a spawn did not touch is free: DOLT_ADD on a clean table
// stages nothing, and the empty-commit guard already skips a commit with
// nothing staged.
func RecurrenceSpawnTables() []string { return []string{"issues", "events", "labels"} }

// SpawnRecurrenceInTx creates the next instance of a recurring bead, and is
// called from the close path so every close — single, checked, or batched —
// reaches it through the one chokepoint.
//
// It returns a zero SpawnResult when nothing was spawned: the bead does not
// repeat, its series has ended, or it lives on the wisp plane (wisps are
// scratch state that is garbage-collected, so respawning one would manufacture
// litter). A genuine write failure IS returned and fails the close, because
// silently losing the successor is how a recurring bead quietly stops
// recurring.
func SpawnRecurrenceInTx(ctx context.Context, tx DBTX, id, actor string) (SpawnResult, error) {
	var result SpawnResult
	issue, err := GetIssueInTx(ctx, tx, id)
	if err != nil {
		return result, fmt.Errorf("spawn recurrence: load %s: %w", id, err)
	}
	if issue == nil || !issue.IsRecurring() || IsWisp(issue) {
		return result, nil
	}

	// The series advances from the instance's OWN due date when it has one, so
	// a bead closed early or late still lands its successor on the schedule
	// rather than relative to when the work happened to finish.
	from := time.Now().UTC()
	if issue.DueAt != nil {
		from = issue.DueAt.UTC()
	}
	next, ok, err := issue.NextOccurrence(from)
	if err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: %w", id, err)
	}
	if !ok {
		return result, nil // series exhausted: repeat_end reached
	}

	if actor == "" {
		actor = RecurrenceSpawnActor
	}
	successor := nextRecurrenceInstance(issue, next, actor)

	prefix, err := ReadConfigPrefix(ctx, tx)
	if err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: read prefix: %w", id, err)
	}
	newID, err := GenerateIssueIDInTable(ctx, tx, "issues", prefix, successor, actor)
	if err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: generate id: %w", id, err)
	}
	successor.ID = newID
	successor.ContentHash = successor.ComputeContentHash()

	if err := insertIssueCreateOnly(ctx, tx, "issues", successor); err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: insert %s: %w", id, newID, err)
	}
	result.ID = newID
	result.ChangedTables = map[string]bool{"issues": true, "events": true}
	// Labels live in their own table, so the row insert above does not carry
	// them. A recurring chore's label set is part of what the work IS — the
	// team that filters on it expects every instance to appear — so the
	// successor gets them through the same helper create uses.
	labelResult, err := PersistLabels(ctx, tx, successor, actor, "events")
	if err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: persist labels on %s: %w", id, newID, err)
	}
	result.ChangedTables = mergeChangedTables(result.ChangedTables, labelResult.ChangedTables)
	if err := RecordEventInTable(ctx, tx, "events", newID, types.EventCreated, actor, ""); err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: record create event: %w", id, err)
	}
	if err := RecordEventInTx(ctx, tx, EventCreate, newID, actor); err != nil {
		return result, err
	}
	// The lineage link lives on the CLOSED bead, so a reader holding any
	// instance can walk the series forward without a schema column for it.
	if err := RecordFullEventInTable(ctx, tx, "events", id, types.EventRecurrenceSpawned,
		actor, id, newID); err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: record lineage event: %w", id, err)
	}
	return result, nil
}

// nextRecurrenceInstance builds the successor bead: the same work, due next.
//
// It copies what DESCRIBES the work and resets what describes this instance of
// it. Deliberately not carried over: status/closure (the successor is open),
// assignment and leases (the next occurrence is unclaimed), external refs and
// spec ids (they identify one instance), and compaction state.
func nextRecurrenceInstance(prev *types.Issue, due time.Time, actor string) *types.Issue {
	now := time.Now().UTC()
	next := &types.Issue{
		Title:              prev.Title,
		Description:        prev.Description,
		Design:             prev.Design,
		AcceptanceCriteria: prev.AcceptanceCriteria,
		Notes:              prev.Notes,
		Status:             types.StatusOpen,
		Priority:           prev.Priority,
		IssueType:          prev.IssueType,
		Owner:              prev.Owner,
		EstimatedMinutes:   prev.EstimatedMinutes,
		CreatedAt:          now,
		CreatedBy:          actor,
		UpdatedAt:          now,
		DueAt:              &due,
		DueSource:          types.DueSourceRepeat,
		RepeatPattern:      prev.RepeatPattern,
		RepeatStart:        prev.RepeatStart,
		RepeatEnd:          prev.RepeatEnd,
		SourceRepo:         prev.SourceRepo,
		StorageClass:       prev.StorageClass,
		MolType:            prev.MolType,
		WorkType:           prev.WorkType,
		Metadata:           prev.Metadata,
		Labels:             append([]string(nil), prev.Labels...),
	}
	// The successor inherits the assignee so a recurring chore stays with
	// whoever owns it; an unassigned series stays unassigned.
	next.Assignee = prev.Assignee
	return next
}

// ScheduledSweepResult aggregates the two lazy time-based sweeps a read path
// runs before serving the ready front: returning expired defers to open, and
// firing beads whose due date has arrived.
type ScheduledSweepResult struct {
	Defers WakeDefersResult
	Due    DueSweepResult
}

// IssueRows reports how many PERMANENT-table rows the sweeps changed. Only
// these decide whether the caller mints a Dolt commit; wisp tables are
// dolt_ignored.
func (r ScheduledSweepResult) IssueRows() int { return len(r.Defers.Issues) + len(r.Due.Issues) }

// WispRows reports how many wisp-plane rows the sweeps changed. A wisp-only
// change still needs a plain SQL commit, but mints no version commit.
func (r ScheduledSweepResult) WispRows() int { return len(r.Defers.Wisps) + len(r.Due.Wisps) }

// CommitMessage names the Dolt commit for a sweep that changed permanent rows.
// Callers with IssueRows() == 0 should not commit at all.
func (r ScheduledSweepResult) CommitMessage() string {
	switch {
	case len(r.Due.Issues) == 0:
		return WakeDefersCommitMessage(len(r.Defers.Issues))
	case len(r.Defers.Issues) == 0:
		return DueSweepCommitMessage(len(r.Due.Issues))
	default:
		return fmt.Sprintf("bd: wake %d expired defer(s), trigger %d due bead(s)",
			len(r.Defers.Issues), len(r.Due.Issues))
	}
}

// RunScheduledSweepsInTx runs both time-based sweeps in one transaction. It is
// what read paths call: the two fire at the same moment, are advisory in the
// same way, and sharing a transaction means a ready listing pays for one write
// round trip rather than two.
//
// The defer wake runs FIRST so a bead whose defer expires and whose due date
// has also passed is returned to open before the due sweep looks at it, and
// fires in the same pass instead of waiting for the next read.
func RunScheduledSweepsInTx(ctx context.Context, tx DBTX) (ScheduledSweepResult, error) {
	var result ScheduledSweepResult
	defers, err := WakeExpiredDefersInTx(ctx, tx)
	if err != nil {
		return result, err
	}
	result.Defers = defers
	due, err := SweepDueBeadsInTx(ctx, tx)
	if err != nil {
		return result, err
	}
	result.Due = due
	return result, nil
}
