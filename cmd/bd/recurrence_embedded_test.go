//go:build cgo

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// bdCreateFail runs "bd create" expecting a refusal, and returns the output so
// the caller can pin WHICH refusal it was.
func bdCreateRecurrenceFail(t *testing.T, bd, dir string, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"create"}, args...)
	cmd := exec.Command(bd, fullArgs...)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected bd create %s to fail, but it succeeded:\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}

// bdIssuesByTitle returns every issue in the workspace whose title matches,
// which is how a spawned successor is found: it carries its predecessor's
// title and gets a fresh id.
func bdIssuesByTitle(t *testing.T, bd, dir, title string) []*types.Issue {
	t.Helper()
	cmd := exec.Command(bd, "list", "--json", "--all", "--limit", "200")
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	stdout, stderr, err := runCommandBuffers(t, cmd)
	if err != nil {
		t.Fatalf("bd list --json failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	s := stdout.String()
	start := strings.Index(s, "[")
	if start < 0 {
		return nil
	}
	var all []*types.Issue
	if err := json.Unmarshal([]byte(s[start:]), &all); err != nil {
		t.Fatalf("parse list JSON: %v\n%s", err, s)
	}
	var matched []*types.Issue
	for _, issue := range all {
		if issue != nil && issue.Title == title {
			matched = append(matched, issue)
		}
	}
	return matched
}

func TestEmbeddedRecurrenceCreate(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "rc")

	t.Run("repeat_fields_round_trip_through_storage", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Weekly standup notes", "--type", "chore",
			"--due", "+1d", "--repeat", "+1w",
			"--repeat-start", "2026-01-01", "--repeat-end", "2030-01-01")
		got := bdShow(t, bd, dir, issue.ID)
		if got.RepeatPattern != "+1w" {
			t.Errorf("repeat_pattern = %q, want +1w", got.RepeatPattern)
		}
		if got.RepeatStart == nil || got.RepeatEnd == nil {
			t.Fatalf("repeat bounds did not persist: start=%v end=%v", got.RepeatStart, got.RepeatEnd)
		}
		if got.RepeatStart.Year() != 2026 || got.RepeatEnd.Year() != 2030 {
			t.Errorf("repeat bounds = %v..%v, want 2026..2030", got.RepeatStart, got.RepeatEnd)
		}
		if got.DueAt == nil {
			t.Error("due_at did not persist")
		}
	})

	t.Run("cron_pattern_round_trips", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Monday review", "--type", "chore", "--repeat", "0 9 * * 1")
		got := bdShow(t, bd, dir, issue.ID)
		if got.RepeatPattern != "0 9 * * 1" {
			t.Errorf("repeat_pattern = %q, want the cron expression", got.RepeatPattern)
		}
	})

	// A --repeat with no --due dates the first instance from the rule itself,
	// so a recurring bead never has to restate its own schedule.
	t.Run("repeat_without_due_takes_its_due_from_the_pattern", func(t *testing.T) {
		before := time.Now().UTC()
		issue := bdCreate(t, bd, dir, "Daily sweep", "--type", "chore", "--repeat", "+1d")
		got := bdShow(t, bd, dir, issue.ID)
		if got.DueAt == nil {
			t.Fatal("recurring bead created with no due date")
		}
		if !got.DueAt.After(before) {
			t.Errorf("derived due %v is not in the future (now %v)", got.DueAt, before)
		}
	})

	t.Run("explicit_due_wins_over_the_pattern", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Explicit due wins", "--type", "task",
			"--due", "2027-05-05", "--repeat", "+1d")
		got := bdShow(t, bd, dir, issue.ID)
		if got.DueAt == nil || got.DueAt.Year() != 2027 {
			t.Errorf("due_at = %v, want the explicit 2027 date", got.DueAt)
		}
	})

	t.Run("unparseable_pattern_is_refused", func(t *testing.T) {
		out := bdCreateRecurrenceFail(t, bd, dir, "Bad rule", "--repeat", "every other tuesday")
		if !strings.Contains(out, "repeat") {
			t.Errorf("refusal did not mention --repeat:\n%s", out)
		}
	})

	t.Run("bounds_without_a_pattern_are_refused", func(t *testing.T) {
		out := bdCreateRecurrenceFail(t, bd, dir, "Orphan bound", "--repeat-end", "2030-01-01")
		if !strings.Contains(out, "--repeat") {
			t.Errorf("refusal did not name --repeat:\n%s", out)
		}
	})

	t.Run("end_before_start_is_refused", func(t *testing.T) {
		out := bdCreateRecurrenceFail(t, bd, dir, "Inverted bounds", "--repeat", "+1d",
			"--repeat-start", "2030-01-01", "--repeat-end", "2029-01-01")
		if !strings.Contains(out, "repeat-end") {
			t.Errorf("refusal did not name the inverted bound:\n%s", out)
		}
	})

	t.Run("update_can_set_and_stop_a_series", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Becomes recurring", "--type", "task", "--due", "+1d")
		bdUpdate(t, bd, dir, issue.ID, "--repeat", "+2w")
		got := bdShow(t, bd, dir, issue.ID)
		if got.RepeatPattern != "+2w" {
			t.Fatalf("repeat_pattern after update = %q, want +2w", got.RepeatPattern)
		}
		// An empty --repeat stops the series; the bead keeps its own due date.
		bdUpdate(t, bd, dir, issue.ID, "--repeat", "")
		got = bdShow(t, bd, dir, issue.ID)
		if got.RepeatPattern != "" {
			t.Errorf("repeat_pattern after clearing = %q, want empty", got.RepeatPattern)
		}
		if got.DueAt == nil {
			t.Error("stopping the series cleared the bead's own due date")
		}
	})

	t.Run("update_refuses_an_unparseable_pattern", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Reject bad update", "--type", "task")
		cmd := exec.Command(bd, "update", issue.ID, "--repeat", "sometimes")
		cmd.Dir = dir
		cmd.Env = bdEnv(dir)
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("expected the update to be refused, got:\n%s", out)
		}
	})
}

func TestEmbeddedRecurrenceSpawnOnClose(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "rs")

	t.Run("closing_a_recurring_bead_files_the_next_instance", func(t *testing.T) {
		const title = "Water the plants"
		first := bdCreate(t, bd, dir, title, "--type", "chore", "--due", "2027-03-01",
			"--repeat", "+1w", "--labels", "household")
		bdClose(t, bd, dir, first.ID)

		matched := bdIssuesByTitle(t, bd, dir, title)
		if len(matched) != 2 {
			t.Fatalf("found %d beads titled %q, want the closed one plus its successor", len(matched), title)
		}
		var successor *types.Issue
		for _, issue := range matched {
			if issue.ID != first.ID {
				successor = issue
			}
		}
		if successor == nil {
			t.Fatal("no successor was created")
		}
		if successor.Status != types.StatusOpen {
			t.Errorf("successor status = %q, want open", successor.Status)
		}
		if successor.RepeatPattern != "+1w" {
			t.Errorf("successor repeat_pattern = %q, want the series to continue", successor.RepeatPattern)
		}
		if successor.DueAt == nil {
			t.Fatal("successor has no due date")
		}
		// The series advances from the closed instance's own due date, not from
		// the moment it happened to be closed.
		want := time.Date(2027, 3, 8, 0, 0, 0, 0, time.UTC)
		if successor.DueAt.UTC().Format("2006-01-02") != want.Format("2006-01-02") {
			t.Errorf("successor due = %v, want %v (one week past the closed instance's due date)",
				successor.DueAt.UTC(), want)
		}
		if successor.DueSource != types.DueSourceRepeat {
			t.Errorf("successor due_source = %q, want %q", successor.DueSource, types.DueSourceRepeat)
		}
		// Labels live in their own table; a successor that carried them only in
		// memory would vanish from every label-filtered view.
		persisted := bdShow(t, bd, dir, successor.ID)
		if len(persisted.Labels) != 1 || persisted.Labels[0] != "household" {
			t.Errorf("successor labels = %v, want [household]", persisted.Labels)
		}

		// The lineage is recorded on the CLOSED bead, so the series reads
		// forward from any instance.
		events := bdHistoryJSON(t, bd, dir, first.ID, "--events")
		if !eventsContain(events, "recurrence_spawned", successor.ID) {
			t.Errorf("no recurrence_spawned event naming %s in:\n%+v", successor.ID, events)
		}
	})

	t.Run("closing_a_non_recurring_bead_spawns_nothing", func(t *testing.T) {
		const title = "One-off cleanup"
		issue := bdCreate(t, bd, dir, title, "--type", "task", "--due", "+1d")
		bdClose(t, bd, dir, issue.ID)
		if matched := bdIssuesByTitle(t, bd, dir, title); len(matched) != 1 {
			t.Errorf("found %d beads titled %q, want only the closed one", len(matched), title)
		}
	})

	// repeat_end is what ENDS a series: once the next occurrence would fall
	// past it, closing the last instance files nothing.
	t.Run("an_exhausted_series_spawns_nothing", func(t *testing.T) {
		const title = "Series that has ended"
		issue := bdCreate(t, bd, dir, title, "--type", "chore",
			"--due", "2027-03-01", "--repeat", "+1w", "--repeat-end", "2027-03-05")
		bdClose(t, bd, dir, issue.ID)
		if matched := bdIssuesByTitle(t, bd, dir, title); len(matched) != 1 {
			t.Errorf("found %d beads titled %q, want no successor past repeat_end", len(matched), title)
		}
	})

	// Closing the successor keeps the chain going, which is what makes this a
	// series rather than a single extra instance.
	t.Run("the_series_continues_past_the_second_instance", func(t *testing.T) {
		const title = "Chain continues"
		first := bdCreate(t, bd, dir, title, "--type", "chore", "--due", "2027-03-01", "--repeat", "+1w")
		bdClose(t, bd, dir, first.ID)
		second := otherThan(t, bdIssuesByTitle(t, bd, dir, title), first.ID)
		bdClose(t, bd, dir, second.ID)
		if matched := bdIssuesByTitle(t, bd, dir, title); len(matched) != 3 {
			t.Errorf("found %d beads titled %q after two closes, want 3", len(matched), title)
		}
	})
}

// eventsContain reports whether any audit event has the given type and mentions
// value somewhere in its payload.
func eventsContain(events []map[string]interface{}, eventType, value string) bool {
	for _, event := range events {
		if fmt.Sprint(event["event_type"]) != eventType {
			continue
		}
		for _, key := range []string{"new_value", "old_value", "comment"} {
			if strings.Contains(fmt.Sprint(event[key]), value) {
				return true
			}
		}
	}
	return false
}

// otherThan returns the single issue in matched whose id is not excludeID.
func otherThan(t *testing.T, matched []*types.Issue, excludeID string) *types.Issue {
	t.Helper()
	var found *types.Issue
	for _, issue := range matched {
		if issue.ID == excludeID {
			continue
		}
		if found != nil {
			t.Fatalf("expected exactly one bead other than %s, found %s and %s", excludeID, found.ID, issue.ID)
		}
		found = issue
	}
	if found == nil {
		t.Fatalf("no bead other than %s", excludeID)
	}
	return found
}

// bdReadyRun runs `bd ready`, the ready-front read the lazy time-based sweeps
// hang off. Its OUTPUT is irrelevant here — running it is the point.
func bdReadyRun(t *testing.T, bd, dir string) {
	t.Helper()
	if out, err := bdRunWithFlockRetry(t, bd, dir, "ready"); err != nil {
		t.Fatalf("bd ready failed: %v\n%s", err, out)
	}
}

func TestEmbeddedDueTrigger(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "dt")

	// A due date that has arrived fires an event an external watcher can
	// consume, and is pushed forward so the deadline keeps nagging rather than
	// sitting silently overdue.
	t.Run("an_arrived_due_date_fires_an_event_and_reschedules", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Overdue work", "--type", "task", "--due", "2020-01-01")
		before := bdShow(t, bd, dir, issue.ID)
		if before.DueAt == nil || before.DueAt.Year() != 2020 {
			t.Fatalf("setup: due_at = %v, want the 2020 date", before.DueAt)
		}

		// `bd ready` is a ready-front read, which is where the lazy sweeps run.
		bdReadyRun(t, bd, dir)

		after := bdShow(t, bd, dir, issue.ID)
		if after.DueAt == nil {
			t.Fatal("due_at was cleared by the sweep")
		}
		if !after.DueAt.After(time.Now().UTC()) {
			t.Errorf("due_at = %v is still in the past; the sweep did not reschedule it", after.DueAt)
		}

		events := bdHistoryJSON(t, bd, dir, issue.ID, "--events")
		if !eventsContain(events, "due", "2020") {
			t.Errorf("no due event naming the fired date in:\n%+v", events)
		}
	})

	t.Run("a_recurring_bead_reschedules_onto_its_own_pattern", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Overdue recurring", "--type", "chore",
			"--due", "2020-01-01", "--repeat", "0 9 * * 1")
		bdReadyRun(t, bd, dir)

		after := bdShow(t, bd, dir, issue.ID)
		if after.DueAt == nil {
			t.Fatal("due_at was cleared by the sweep")
		}
		if after.DueAt.UTC().Weekday() != time.Monday || after.DueAt.UTC().Hour() != 9 {
			t.Errorf("rescheduled due = %v, want the next Monday 09:00 from the pattern", after.DueAt.UTC())
		}
	})

	// A future deadline is not due, so the sweep must leave it entirely alone.
	t.Run("a_future_due_date_is_untouched", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Not yet due", "--type", "task", "--due", "2030-06-01")
		bdReadyRun(t, bd, dir)
		after := bdShow(t, bd, dir, issue.ID)
		if after.DueAt == nil || after.DueAt.Year() != 2030 {
			t.Errorf("due_at = %v, want the untouched 2030 date", after.DueAt)
		}
		events := bdHistoryJSON(t, bd, dir, issue.ID, "--events")
		if eventsContain(events, "due", "") {
			t.Errorf("a not-yet-due bead fired a due event:\n%+v", events)
		}
	})

	// The sweep fires once per arrival, not once per read: the reschedule is
	// what makes it idempotent.
	t.Run("a_second_read_does_not_refire_the_same_deadline", func(t *testing.T) {
		issue := bdCreate(t, bd, dir, "Fires once", "--type", "task", "--due", "2020-01-01")
		bdReadyRun(t, bd, dir)
		firstDue := bdShow(t, bd, dir, issue.ID).DueAt
		bdReadyRun(t, bd, dir)
		secondDue := bdShow(t, bd, dir, issue.ID).DueAt
		if firstDue == nil || secondDue == nil {
			t.Fatalf("due dates went missing: %v, %v", firstDue, secondDue)
		}
		if !firstDue.Equal(*secondDue) {
			t.Errorf("second read moved the deadline again: %v -> %v", firstDue, secondDue)
		}
	})
}

// bdDueBackfillJSON runs `bd due backfill --json` and decodes the report.
func bdDueBackfillJSON(t *testing.T, bd, dir string, args ...string) dueBackfillReport {
	t.Helper()
	fullArgs := append([]string{"due", "backfill", "--json"}, args...)
	out, err := bdRunWithFlockRetry(t, bd, dir, fullArgs...)
	if err != nil {
		t.Fatalf("bd due backfill failed: %v\n%s", err, out)
	}
	s := string(out)
	start := strings.Index(s, "{")
	if start < 0 {
		t.Fatalf("no JSON object in backfill output:\n%s", s)
	}
	var report dueBackfillReport
	if err := json.Unmarshal([]byte(s[start:]), &report); err != nil {
		t.Fatalf("parse backfill JSON: %v\n%s", err, s)
	}
	return report
}

func TestEmbeddedDueBackfill(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "bf")

	// Beads created without --due under the default posture carry no due date:
	// exactly the legacy shape the backfill exists for.
	undated := bdCreate(t, bd, dir, "Legacy work item", "--type", "task")
	dated := bdCreate(t, bd, dir, "Already dated", "--type", "task", "--due", "2030-01-01")
	if bdShow(t, bd, dir, undated.ID).DueAt != nil {
		t.Skip("this workspace mints due dates at create; the backfill has nothing to fix")
	}

	t.Run("the_default_run_reports_and_writes_nothing", func(t *testing.T) {
		report := bdDueBackfillJSON(t, bd, dir)
		if report.Applied {
			t.Error("a default run reported itself applied; it must be a dry run")
		}
		if report.Updated != 0 {
			t.Errorf("Updated = %d, want 0 for a dry run", report.Updated)
		}
		if report.Candidates < 1 {
			t.Fatalf("Candidates = %d, want at least the undated bead", report.Candidates)
		}
		if report.AlreadyDated < 1 {
			t.Errorf("AlreadyDated = %d, want at least the dated bead", report.AlreadyDated)
		}
		// The crucial assertion: the database is unchanged.
		if got := bdShow(t, bd, dir, undated.ID); got.DueAt != nil {
			t.Errorf("dry run wrote a due date: %v", got.DueAt)
		}
	})

	t.Run("apply_writes_the_dates_the_report_described", func(t *testing.T) {
		plan := bdDueBackfillJSON(t, bd, dir)
		applied := bdDueBackfillJSON(t, bd, dir, "--apply")
		if !applied.Applied {
			t.Error("an --apply run did not report itself applied")
		}
		if applied.Updated != plan.Candidates {
			t.Errorf("Updated = %d, want the %d the plan described", applied.Updated, plan.Candidates)
		}
		if len(applied.Failed) != 0 {
			t.Errorf("backfill failures: %v", applied.Failed)
		}

		got := bdShow(t, bd, dir, undated.ID)
		if got.DueAt == nil {
			t.Fatal("backfill left the undated bead without a due date")
		}
		if got.DueSource != types.DueSourceBackfill {
			t.Errorf("due_source = %q, want %q", got.DueSource, types.DueSourceBackfill)
		}
		// Dated from the bead's OWN creation, a week out by default.
		if want := got.CreatedAt.Add(7 * 24 * time.Hour); got.DueAt.UTC().Sub(want).Abs() > time.Minute {
			t.Errorf("due_at = %v, want %v (created_at + 7d)", got.DueAt.UTC(), want)
		}

		// A bead that already had a date is never overwritten.
		if still := bdShow(t, bd, dir, dated.ID); still.DueAt == nil || still.DueAt.Year() != 2030 {
			t.Errorf("backfill overwrote an existing due date: %v", still.DueAt)
		}
	})

	t.Run("a_second_run_finds_nothing_left_to_do", func(t *testing.T) {
		report := bdDueBackfillJSON(t, bd, dir)
		if report.Candidates != 0 {
			t.Errorf("Candidates = %d after a backfill, want 0 (rows: %+v)", report.Candidates, report.Rows)
		}
	})
}

// The JSONL interchange is how beads move between clones, so a recurring bead
// that exports without its rule arrives somewhere else as a one-off.
func TestEmbeddedRecurrenceSurvivesExportImport(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	source, _, _ := bdInit(t, bd, "--prefix", "rx")

	issue := bdCreate(t, bd, source, "Exported recurring bead", "--type", "chore",
		"--due", "2027-04-01", "--repeat", "0 9 * * 1",
		"--repeat-start", "2027-01-01", "--repeat-end", "2029-01-01")

	exported := filepath.Join(t.TempDir(), "issues.jsonl")
	if out, err := bdRunWithFlockRetry(t, bd, source, "export", "-o", exported); err != nil {
		t.Fatalf("bd export failed: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(exported)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(raw), `"repeat_pattern":"0 9 * * 1"`) {
		t.Fatalf("export dropped the repeat pattern:\n%s", raw)
	}

	target, _, _ := bdInit(t, bd, "--prefix", "rx")
	if out, err := bdRunWithFlockRetry(t, bd, target, "import", exported); err != nil {
		t.Fatalf("bd import failed: %v\n%s", err, out)
	}

	got := bdShow(t, bd, target, issue.ID)
	if got.RepeatPattern != "0 9 * * 1" {
		t.Errorf("imported repeat_pattern = %q, want the cron expression", got.RepeatPattern)
	}
	if got.RepeatStart == nil || got.RepeatStart.Year() != 2027 {
		t.Errorf("imported repeat_start = %v, want 2027", got.RepeatStart)
	}
	if got.RepeatEnd == nil || got.RepeatEnd.Year() != 2029 {
		t.Errorf("imported repeat_end = %v, want 2029", got.RepeatEnd)
	}
}
