package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dberrors"
	"github.com/steveyegge/beads/internal/storage/depid"
	"github.com/steveyegge/beads/internal/timeparsing"
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

// DueMissEscalateAt is how many misses it takes before a bead's priority is
// raised once.
//
// Rescheduling alone (DueMissGrace above) keeps a deadline visible, but at a
// FIXED priority: a bead missed once and a bead missed twenty times sit at the
// same place in the ready front, so a chronically missed deadline is
// indistinguishable from a fresh one and nothing ever changes. One raise at a
// small threshold is the whole escalation — it moves the bead where a human
// looks, once, and then stops. It does NOT walk the bead up to P0 over
// successive misses: an escalation that repeats is just the nag again, one
// rung higher each time, and would eventually flatten every priority in the
// workspace to P0.
//
// Three is the smallest count that distinguishes a pattern from an accident:
// one miss is a bad day, two is bad luck, three is the deadline being wrong or
// the work being stuck, and either wants a person.
const DueMissEscalateAt = 3

// DueSweepResult reports what one sweep fired, per table. Issues are
// permanent-table ids and are the only ones that decide whether the caller
// mints a Dolt commit; wisp tables are dolt_ignored.
type DueSweepResult struct {
	Issues []string
	Wisps  []string
	// Escalated names the beads whose miss count reached DueMissEscalateAt in
	// THIS sweep and whose priority was therefore raised — split by plane the
	// same way Issues and Wisps are, and a SUBSET of them. It is what a clock
	// publishes as the actionable half of its summary: "fired 9, escalated 1"
	// says where to look, where "fired 9" alone does not.
	Escalated      []string
	EscalatedWisps []string
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
	issues, escalated, err := sweepDueBeadsInTable(ctx, tx, "issues", "events", live)
	if err != nil {
		return result, err
	}
	result.Issues = issues
	result.Escalated = escalated
	wisps, escalatedWisps, err := sweepDueBeadsInTable(ctx, tx, "wisps", "wisp_events", live)
	if err != nil {
		if dberrors.IsTableNotExist(err) {
			return result, nil
		}
		return result, err
	}
	result.Wisps = wisps
	result.EscalatedWisps = escalatedWisps
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
	dueMissed     int
	priority      int
}

// the status placeholders are generated, never interpolated values.
//
//nolint:gosec // G201: table and eventsTable are hardcoded constants from the caller;
func sweepDueBeadsInTable(ctx context.Context, tx DBTX, table, eventsTable string, live []types.Status) (fired, escalated []string, err error) {
	if len(live) == 0 {
		return nil, nil, nil
	}
	statusList, statusArgs := statusPlaceholders(live)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, due_at, repeat_pattern, repeat_start, repeat_end, due_missed, priority
		FROM %s
		WHERE due_at IS NOT NULL AND due_at <= UTC_TIMESTAMP()
		  AND status IN (%s)
	`, table, statusList), statusArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("due sweep: scan %s: %w", table, err)
	}
	var candidates []dueCandidate
	for rows.Next() {
		var c dueCandidate
		var pattern sql.NullString
		var start, end sql.NullTime
		if err := rows.Scan(&c.id, &c.dueAt, &pattern, &start, &end, &c.dueMissed, &c.priority); err != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("due sweep: scan %s row: %w", table, err)
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
		return nil, nil, fmt.Errorf("due sweep: iterate %s: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, fmt.Errorf("due sweep: close %s rows: %w", table, err)
	}
	if len(candidates) == 0 {
		return nil, nil, nil
	}

	now := time.Now().UTC()
	for _, c := range candidates {
		next, fromPattern := nextDueAfterMiss(c, now)
		// The predicate is repeated so a bead closed, deferred, or rescheduled
		// between the SELECT and here matches nothing and is left alone rather
		// than clobbered.
		//
		// due_source records where the date CAME from, so only a date the
		// pattern computed is stamped repeat; a grace push — on a non-recurring
		// bead, or on a series that has ended — leaves the provenance in place.
		sourceClause := ""
		args := []any{next}
		if fromPattern {
			sourceClause = ", due_source = ?"
			args = append(args, string(types.DueSourceRepeat))
		}

		// The miss is counted on the same UPDATE that reschedules, so a bead
		// can never be rescheduled without its miss being recorded: a counter
		// written separately could be lost to a crash in between, and the
		// escalation would then never arrive for a bead that had genuinely
		// missed its threshold.
		//
		// The raise fires on the sweep that REACHES the threshold and on no
		// other, so escalation happens once per bead rather than on every
		// subsequent miss (see DueMissEscalateAt). A bead already at P0 has
		// nowhere to go and is counted, rescheduled, and left at P0.
		missed := c.dueMissed + 1
		raisedTo, raised := escalatedPriority(c.priority, missed)
		priorityClause := ""
		if raised {
			priorityClause = ", priority = ?"
			args = append(args, raisedTo)
		}

		args = append(args, missed, now, freshRowLock(), c.id, c.dueAt)
		args = append(args, statusArgs...)
		res, err := tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE %s
			SET due_at = ?%s%s, due_missed = ?, updated_at = ?, row_lock = ?
			WHERE id = ? AND due_at = ? AND status IN (%s)
		`, table, sourceClause, priorityClause, statusList), args...)
		if err != nil {
			return fired, escalated, fmt.Errorf("due sweep reschedule %s: %w", c.id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fired, escalated, fmt.Errorf("due sweep reschedule %s rows affected: %w", c.id, err)
		}
		if n == 0 {
			continue // rescued concurrently — leave it be
		}
		// The event carries the miss count, so a consumer reading the rail
		// sees a first miss and a third one as different facts without
		// re-reading the bead.
		comment := fmt.Sprintf("missed %d", missed)
		if raised {
			comment = fmt.Sprintf("missed %d; priority raised to P%d", missed, raisedTo)
		}
		if err := RecordFullEventWithCommentInTable(ctx, tx, eventsTable, c.id, types.EventDue,
			DueTriggerActor, c.dueAt.UTC().Format(time.RFC3339), next.Format(time.RFC3339), comment); err != nil {
			return fired, escalated, fmt.Errorf("record due event for %s: %w", c.id, err)
		}
		// A reschedule is a field change, so it journals as an update — past
		// the rows-affected recheck, so a concurrently-rescued bead records
		// nothing.
		if err := RecordEventInTx(ctx, tx, EventUpdate, c.id, DueTriggerActor); err != nil {
			return fired, escalated, err
		}
		fired = append(fired, c.id)
		if raised {
			escalated = append(escalated, c.id)
		}
	}
	return fired, escalated, nil
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

// escalatedPriority decides whether THIS miss raises the bead's priority, and
// to what.
//
// It raises on the miss that REACHES DueMissEscalateAt and on no other, which
// is what makes the escalation a step rather than a ramp: `>=` would raise
// again on every subsequent miss and walk a chronically-late bead to P0 one
// rung per miss, until a workspace with a stale backlog has nothing but P0s
// and the priority field has stopped meaning anything.
//
// P0 is the ceiling and is expressed as "there is somewhere to go" rather than
// as a named floor value: a bead already at the top is counted and rescheduled
// like any other, and simply has no raise to apply.
func escalatedPriority(current, missed int) (raised int, ok bool) {
	if missed != DueMissEscalateAt || current <= 0 {
		return current, false
	}
	return current - 1, true
}

// nextDueAfterMiss computes where a fired bead's due date moves to. A recurring
// bead follows its own pattern; everything else — including a recurring bead
// whose series has ended or whose pattern no longer parses — gets one
// DueMissGrace, so a deadline keeps nagging instead of going silent.
//
// The second result reports whether the PATTERN produced the date, so a grace
// fallback is never labeled as pattern-computed.
func nextDueAfterMiss(c dueCandidate, now time.Time) (time.Time, bool) {
	if c.repeatPattern != "" {
		probe := &types.Issue{
			RepeatPattern: c.repeatPattern,
			RepeatStart:   c.repeatStart,
			RepeatEnd:     c.repeatEnd,
		}
		if next, ok := nextOccurrencePast(probe, c.dueAt, now); ok {
			return next, true
		}
	}
	return now.Add(DueMissGrace), false
}

// nextOccurrencePast finds the first occurrence of a series that lies strictly
// after now, walking forward from the instance due at dueAt, or ok=false when
// the series cannot supply one: it has ended (repeat_end), its pattern is
// unusable, or the walk would exceed RepeatHorizon.
//
// An INTERVAL rule steps from dueAt by its own interval, as many times as it
// takes to clear now, so the schedule keeps its phase: a weekly bead due
// Monday 09:00 that is swept or closed on a Wednesday lands on the following
// Monday 09:00, however late it was. A CRON rule already expresses its phase,
// so it matches from the later of dueAt and now — the same answer the walk
// would reach, without visiting every occurrence in between.
func nextOccurrencePast(probe *types.Issue, dueAt, now time.Time) (time.Time, bool) {
	repeat, err := probe.Repeat()
	if err != nil {
		return time.Time{}, false
	}
	from := dueAt
	if !repeat.IsInterval() && now.After(from) {
		from = now
	}
	return walkOccurrences(probe, from, now, from.Add(timeparsing.RepeatHorizon))
}

// nextSeriesOccurrence is nextOccurrencePast walked from a series anchor
// rather than from an instance's current due date: it steps the pattern from
// anchor and returns the first occurrence strictly after now, bounded by the
// later of anchor and now plus RepeatHorizon. Walking from the recorded
// anchor keeps a close's answer independent of the due sweep, which re-dates
// an open overdue recurring bead on every ready-front read — anchoring on
// that swept date instead would drop the occurrence the sweep had teed up
// whenever a read fell between the deadline and the close.
func nextSeriesOccurrence(probe *types.Issue, anchor, now time.Time) (time.Time, bool) {
	repeat, err := probe.Repeat()
	if err != nil {
		return time.Time{}, false
	}
	from := anchor
	if !repeat.IsInterval() && now.After(from) {
		from = now
	}
	horizon := from
	if now.After(horizon) {
		horizon = now
	}
	return walkOccurrences(probe, from, now, horizon.Add(timeparsing.RepeatHorizon))
}

// walkOccurrences steps a series forward from `from`, returning the first
// occurrence strictly after `now` and within `horizon`, or ok=false when the
// series cannot supply one: it has ended (repeat_end), its pattern is
// unusable, or the next occurrence falls beyond the horizon.
func walkOccurrences(probe *types.Issue, from, now, horizon time.Time) (time.Time, bool) {
	for {
		next, ok, err := probe.NextOccurrence(from)
		if err != nil || !ok || !next.After(from) || next.After(horizon) {
			return time.Time{}, false
		}
		if next.After(now) {
			return next, true
		}
		from = next
	}
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
// fixed set — the successor is one issues row, its audit events, its labels,
// and the parent-child edge it inherits — so a close path that stages a static
// list can simply include it. Staging a table a spawn did not touch is free:
// DOLT_ADD on a clean table stages nothing, and the empty-commit guard already
// skips a commit with nothing staged.
func RecurrenceSpawnTables() []string { return []string{"issues", "events", "labels", "dependencies"} }

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

	// The series advances from its recorded anchor, never from the due date a
	// due sweep may have advanced past the deadline: the sweep re-dates an
	// open overdue recurring bead on every ready-front read, and anchoring on
	// that date would make the successor depend on how much read traffic
	// happened between the deadline and this close, silently dropping the
	// occurrence the sweep had teed up. repeat_start is the anchor — create,
	// update, and every successor record it — with the instance's own due
	// date as the fallback for series that predate it. The walk still ends
	// strictly in the future, so a late close never files an overdue bead,
	// and a successor never lands BEFORE the closed instance's own due date:
	// an early close keeps the occurrence after THAT date rather than
	// re-filing a slot the series has already passed.
	if _, err := issue.Repeat(); err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: %w", id, err)
	}
	now := time.Now().UTC()
	anchor := now
	if issue.DueAt != nil {
		anchor = issue.DueAt.UTC()
	}
	if issue.RepeatStart != nil {
		anchor = issue.RepeatStart.UTC()
	}
	next, ok := nextSeriesOccurrence(issue, anchor, now)
	if ok && issue.DueAt != nil && next.Before(issue.DueAt.UTC()) {
		next, ok = nextSeriesOccurrence(issue, issue.DueAt.UTC(), now)
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
	if err := carryParentLink(ctx, tx, id, newID, actor, result.ChangedTables); err != nil {
		return result, fmt.Errorf("spawn recurrence for %s: carry parent link to %s: %w", id, newID, err)
	}
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

// carryParentLink files the successor under the same parent as the closed
// instance, so a recurring child of an epic stays in that epic.
//
// Recurrence is parent-linked and otherwise flat: peer dependency edges
// (blocks, related, discovered-from, waits-for) describe one instance's
// relationship to other work and are deliberately NOT copied. A successor that
// inherited a `blocks` edge would re-block a bead the closed instance already
// unblocked; a series that needs standing peer edges adds them per instance.
func carryParentLink(ctx context.Context, tx DBTX, prevID, nextID, actor string, changed map[string]bool) error {
	deps, err := GetDependencyRecordsForIssuesInTx(ctx, tx, []string{prevID})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, dep := range deps[prevID] {
		if dep.Type != types.DepParentChild {
			continue
		}
		// The same insert create uses for a --parent edge, on any DBTX: the
		// successor is a brand-new row, so the cycle and hierarchy checks a
		// general dependency add performs have nothing to find.
		edge := &types.Dependency{IssueID: nextID, DependsOnID: dep.DependsOnID, Type: types.DepParentChild}
		kind := ClassifyDepTarget(ctx, tx, edge, types.ExtractPrefix(nextID) != types.ExtractPrefix(dep.DependsOnID))
		//nolint:gosec // G201: the target column comes from DepTargetKind.Column(), a fixed set.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO dependencies (id, issue_id, %s, type, created_by, created_at, metadata, thread_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE type = type
		`, kind.Column()), depid.New(nextID, dep.DependsOnID), nextID, dep.DependsOnID, edge.Type, actor, now, "{}", ""); err != nil {
			return fmt.Errorf("insert parent-child edge %s -> %s: %w", nextID, dep.DependsOnID, err)
		}
		if err := TouchDependencyCoordinationTableInTx(ctx, tx, dep.DependsOnID, "dependencies"); err != nil {
			return err
		}
		if err := RecordDepEventInTx(ctx, tx, EventDepAdd, nextID, string(edge.Type), dep.DependsOnID, "{}", actor); err != nil {
			return err
		}
		changed["dependencies"] = true
	}
	return nil
}

// nextRecurrenceInstance builds the successor bead: the same work, due next.
//
// It copies what DESCRIBES the work and resets what describes this instance of
// it. Deliberately not carried over: status/closure (the successor is open),
// leases (the next occurrence is unclaimed until someone picks it up, though
// it keeps its assignee), external refs and spec ids (they identify one
// instance), and compaction state. Dependency edges are handled by
// carryParentLink: the parent link is kept, peer edges are not.
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

// ClearRecurrenceBoundsOnStop makes an empty repeat_pattern also clear both
// bounds, so stopping a series never leaves a row holding repeat_start or
// repeat_end with no pattern — a shape PrepareIssueForInsert refuses on the
// next export/import.
func ClearRecurrenceBoundsOnStop(updates map[string]interface{}) {
	if pattern, ok := updates["repeat_pattern"].(string); ok && pattern == "" {
		updates["repeat_start"] = nil
		updates["repeat_end"] = nil
	}
}

// AnchorRecurrenceUpdate is AnchorRecurrence for the update funnel: an update
// that gives a bead a repeat pattern with no repeat_start, on a bead that has
// (or is being given) a due date, records that due date as the series' start
// so later steps have their anchor. It runs before ValidateRecurrenceUpdate,
// on the landed triple.
func AnchorRecurrenceUpdate(oldIssue *types.Issue, updates map[string]interface{}) {
	pattern, _ := updates["repeat_pattern"].(string)
	if pattern == "" {
		return
	}
	if raw, ok := updates["repeat_start"]; ok {
		if raw != nil {
			return
		}
	} else if oldIssue.RepeatStart != nil {
		return
	}
	due := oldIssue.DueAt
	if raw, ok := updates["due_at"]; ok {
		if due, _ = updateTimeValue("due_at", raw); due == nil {
			return
		}
	}
	if due == nil {
		return
	}
	updates["repeat_start"] = due.UTC()
}

// ValidateRecurrenceUpdate checks the (repeat_pattern, repeat_start,
// repeat_end) triple an update would LAND — the row's current values merged
// with the update — against the same rule every create path applies
// (types.Issue.ValidateRecurrence), so an update cannot leave a row that a
// create would have refused.
func ValidateRecurrenceUpdate(oldIssue *types.Issue, updates map[string]interface{}) error {
	rawPattern, hasPattern := updates["repeat_pattern"]
	rawStart, hasStart := updates["repeat_start"]
	rawEnd, hasEnd := updates["repeat_end"]
	if !hasPattern && !hasStart && !hasEnd {
		return nil
	}
	merged := types.Issue{
		RepeatPattern: oldIssue.RepeatPattern,
		RepeatStart:   oldIssue.RepeatStart,
		RepeatEnd:     oldIssue.RepeatEnd,
	}
	if hasPattern {
		pattern, ok := rawPattern.(string)
		if !ok {
			return fmt.Errorf("%w: invalid repeat pattern %v", storage.ErrValidation, rawPattern)
		}
		merged.RepeatPattern = pattern
	}
	var err error
	if hasStart {
		if merged.RepeatStart, err = updateTimeValue("repeat_start", rawStart); err != nil {
			return err
		}
	}
	if hasEnd {
		if merged.RepeatEnd, err = updateTimeValue("repeat_end", rawEnd); err != nil {
			return err
		}
	}
	if err := merged.ValidateRecurrence(); err != nil {
		return fmt.Errorf("%w: %v", storage.ErrValidation, err)
	}
	return nil
}

// updateTimeValue reads a nullable timestamp as the update funnels carry it:
// nil clears, and either a time.Time or a *time.Time sets.
func updateTimeValue(key string, raw interface{}) (*time.Time, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case time.Time:
		return &value, nil
	case *time.Time:
		return value, nil
	default:
		return nil, fmt.Errorf("%w: invalid %s value %v", storage.ErrValidation, key, raw)
	}
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
