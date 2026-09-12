package issueops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	publicops "github.com/steveyegge/beads/issueops"
)

// withDueRequired turns the mandatory-due invariant on or off for one test and
// restores the process-wide config singleton afterwards. It is not parallel-safe,
// which is why none of the tests in this file call t.Parallel.
func withDueRequired(t *testing.T, enabled bool) {
	t.Helper()
	t.Chdir(t.TempDir())
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	config.Set(DueRequiredKey, enabled)
	t.Cleanup(config.ResetForTesting)
	if got := DueRequiredEnabled(); got != enabled {
		t.Fatalf("DueRequiredEnabled() = %v, want %v", got, enabled)
	}
}

func dueLessTask() *types.Issue {
	return &types.Issue{ID: "bd-due1", Title: "no deadline", IssueType: types.TypeTask, Status: types.StatusOpen}
}

// The invariant is ON in a workspace that has never configured it: a fresh
// config refuses a due-less task, and only an explicit `due.required: false`
// lets one through.
func TestDueRequiredDefaultsOn(t *testing.T) {
	t.Chdir(t.TempDir())
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	t.Cleanup(config.ResetForTesting)
	if !DueRequiredEnabled() {
		t.Fatal("due.required must default to true")
	}
	if err := ValidateDueRequired(dueLessTask()); err == nil {
		t.Fatal("a fresh workspace must refuse a due-less task")
	}
	config.Set(DueRequiredKey, false)
	if err := ValidateDueRequired(dueLessTask()); err != nil {
		t.Fatalf("invariant off must accept a due-less task, got %v", err)
	}
}

// due_source answers "did a human pick this date": a create that arrives with
// a date and no provenance is stamped explicit, and a path that already named
// its source (the ladder, the backfill, a spawn) is left alone.
func TestStampExplicitDueSource(t *testing.T) {
	issue := dueLessTask()
	StampExplicitDueSource(issue)
	if issue.DueSource != "" {
		t.Fatalf("a due-less issue must not get a source, got %q", issue.DueSource)
	}
	due := time.Now().Add(time.Hour)
	issue.DueAt = &due
	StampExplicitDueSource(issue)
	if issue.DueSource != types.DueSourceExplicit {
		t.Fatalf("due_source = %q, want %q", issue.DueSource, types.DueSourceExplicit)
	}
	issue.DueSource = types.DueSourceDefault
	StampExplicitDueSource(issue)
	if issue.DueSource != types.DueSourceDefault {
		t.Fatalf("an existing source must be kept, got %q", issue.DueSource)
	}

	withDueRequired(t, true)
	request := publicops.CreateRequest{Actor: "seat-a", Issue: &types.Issue{ID: "bd-src", Title: "dated", IssueType: types.TypeTask, Status: types.StatusOpen, DueAt: &due}}
	prepared, err := PreparePublicCreateRequest(request, PublicCreateContext{IssuePrefix: "bd"})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Issue.DueSource != types.DueSourceExplicit {
		t.Fatalf("public create due_source = %q, want %q", prepared.Issue.DueSource, types.DueSourceExplicit)
	}
}

func TestValidateDueRequiredRejectsDuelessWork(t *testing.T) {
	withDueRequired(t, true)
	err := ValidateDueRequired(dueLessTask())
	if err == nil {
		t.Fatal("expected a due-less task to be refused")
	}
	if !errors.Is(err, storage.ErrValidation) {
		t.Fatalf("refusal must be ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "due date is required") {
		t.Fatalf("refusal must name the invariant, got %v", err)
	}
}

func TestValidateDueRequiredAcceptsExplicitDue(t *testing.T) {
	withDueRequired(t, true)
	issue := dueLessTask()
	due := time.Now().Add(24 * time.Hour)
	issue.DueAt = &due
	if err := ValidateDueRequired(issue); err != nil {
		t.Fatalf("a task carrying a due date must be accepted, got %v", err)
	}
}

func TestDueRequiredExemptions(t *testing.T) {
	withDueRequired(t, true)
	exempt := map[string]func(*types.Issue){
		"event type":     func(i *types.Issue) { i.IssueType = types.TypeEvent },
		"ephemeral wisp": func(i *types.Issue) { i.Ephemeral = true },
		"no-history wisp": func(i *types.Issue) {
			i.NoHistory = true
			i.StorageClass = types.StorageClassEphemeral
		},
		"typed wisp":        func(i *types.Issue) { i.WispType = types.WispTypeHeartbeat },
		"ephemeral class":   func(i *types.Issue) { i.StorageClass = types.StorageClassEphemeral },
		"molecule template": func(i *types.Issue) { i.IsTemplate = true },
		"federation row":    func(i *types.Issue) { i.SourceSystem = "github" },
	}
	for name, mark := range exempt {
		t.Run(name, func(t *testing.T) {
			issue := dueLessTask()
			mark(issue)
			if !DueRequiredExempt(issue) {
				t.Fatalf("%s must be exempt from the mandatory-due invariant", name)
			}
			if err := ValidateDueRequired(issue); err != nil {
				t.Fatalf("%s must be accepted without a due date, got %v", name, err)
			}
		})
	}
}

// TestValidatePublicCreateRequestEnforcesDueRequired is the storage-boundary
// case: a create arriving through the public request type — HTTP, MCP, the
// issueops facade — never touches a CLI flag, and is refused all the same.
func TestValidatePublicCreateRequestEnforcesDueRequired(t *testing.T) {
	withDueRequired(t, true)
	request := publicops.CreateRequest{Actor: "seat-a", Issue: dueLessTask()}
	err := ValidatePublicCreateRequest(request)
	if err == nil || !strings.Contains(err.Error(), "due date is required") {
		t.Fatalf("public create must be refused without a due date, got %v", err)
	}

	due := time.Now().Add(72 * time.Hour)
	request.Issue.DueAt = &due
	if err := ValidatePublicCreateRequest(request); err != nil {
		t.Fatalf("public create with a due date must be accepted, got %v", err)
	}
}

// Clearing a due date is the one edit that can undo the invariant, so both
// write funnels refuse it without a reason while the rule is on; the same
// clear with a reason, on an exempt row, or with the rule off, goes through.
func TestValidateDueClear(t *testing.T) {
	withDueRequired(t, true)
	due := time.Now().Add(time.Hour)
	dated := &types.Issue{ID: "bd-c1", Title: "dated", IssueType: types.TypeTask, Status: types.StatusOpen, DueAt: &due}
	clear := map[string]interface{}{"due_at": nil}

	err := ValidateDueClear(dated, clear, "")
	if err == nil || !errors.Is(err, storage.ErrValidation) || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("a clear without a reason must be refused as ErrValidation naming the reason, got %v", err)
	}
	if err := ValidateDueClear(dated, clear, "tracked upstream"); err != nil {
		t.Fatalf("a clear with a reason must be accepted, got %v", err)
	}
	if err := ValidateDueClear(dated, map[string]interface{}{"due_at": due.Add(time.Hour)}, ""); err != nil {
		t.Fatalf("moving a due date is not a clear, got %v", err)
	}
	if err := ValidateDueClear(dated, map[string]interface{}{"title": "x"}, ""); err != nil {
		t.Fatalf("an update that does not touch due_at is not a clear, got %v", err)
	}
	wisp := &types.Issue{ID: "bd-c2", Title: "scratch", IssueType: types.TypeTask, Status: types.StatusOpen, Ephemeral: true, DueAt: &due}
	if err := ValidateDueClear(wisp, clear, ""); err != nil {
		t.Fatalf("an exempt row is not held to the gate, got %v", err)
	}

	updates := map[string]interface{}{"due_at": nil, OpDueClearReason: "  tracked upstream  "}
	if got := PopDueClearReason(updates); got != "tracked upstream" {
		t.Fatalf("PopDueClearReason = %q, want the trimmed reason", got)
	}
	if _, still := updates[OpDueClearReason]; still {
		t.Fatal("the reason op must be popped before the field allowlist sees it")
	}

	withDueRequired(t, false)
	if err := ValidateDueClear(dated, clear, ""); err != nil {
		t.Fatalf("with the invariant off, clearing a due date is unchanged, got %v", err)
	}
}

// A recurring create records its first due date as the series' start, so a
// monthly rule keeps its original day-of-month; an explicit start, a
// non-recurring bead, and a due-less bead are left alone.
func TestAnchorRecurrence(t *testing.T) {
	due := time.Date(2026, 1, 30, 9, 0, 0, 0, time.UTC)
	recurring := &types.Issue{RepeatPattern: "+1m", DueAt: &due}
	AnchorRecurrence(recurring)
	if recurring.RepeatStart == nil || !recurring.RepeatStart.Equal(due) {
		t.Fatalf("repeat_start = %v, want the first due date %v", recurring.RepeatStart, due)
	}
	start := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	explicit := &types.Issue{RepeatPattern: "+1m", DueAt: &due, RepeatStart: &start}
	AnchorRecurrence(explicit)
	if !explicit.RepeatStart.Equal(start) {
		t.Errorf("an explicit start must win, got %v", explicit.RepeatStart)
	}
	plain := &types.Issue{DueAt: &due}
	AnchorRecurrence(plain)
	if plain.RepeatStart != nil {
		t.Errorf("a non-recurring bead must not get a start, got %v", plain.RepeatStart)
	}
	undated := &types.Issue{RepeatPattern: "+1m"}
	AnchorRecurrence(undated)
	if undated.RepeatStart != nil {
		t.Errorf("a due-less bead has nothing to anchor on, got %v", undated.RepeatStart)
	}
}

// TestCreateIssueInTxHonorsSkipDueRequired covers the classic/store leg and the
// `bd import` exemption that rides on it. The refusal happens before any SQL, so
// the mock needs no expectations; the exempt case is asserted by the ABSENCE of
// the due refusal, since it goes on to fail at the first (unexpected) query.
func TestCreateIssueInTxHonorsSkipDueRequired(t *testing.T) {
	withDueRequired(t, true)
	ctx := context.Background()

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	enforcing := &BatchContext{Opts: storage.BatchCreateOptions{}}
	if _, err := CreateIssueInTxWithResult(ctx, db, enforcing, dueLessTask(), "seat-a"); err == nil ||
		!strings.Contains(err.Error(), "due date is required") {
		t.Fatalf("the classic create leg must refuse a due-less issue, got %v", err)
	}

	importing := &BatchContext{Opts: storage.BatchCreateOptions{SkipDueRequired: true}}
	_, err = CreateIssueInTxWithResult(ctx, db, importing, dueLessTask(), "seat-a")
	if err != nil && strings.Contains(err.Error(), "due date is required") {
		t.Fatalf("SkipDueRequired must exempt the import path, got %v", err)
	}
}
