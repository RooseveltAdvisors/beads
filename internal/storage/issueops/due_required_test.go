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

func TestDueRequiredDefaultsOff(t *testing.T) {
	withDueRequired(t, false)
	if err := ValidateDueRequired(dueLessTask()); err != nil {
		t.Fatalf("invariant off must accept a due-less task, got %v", err)
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

func TestDefaultDueRequiredAssignee(t *testing.T) {
	withDueRequired(t, true)

	unowned := dueLessTask()
	DefaultDueRequiredAssignee(unowned, "seat-a")
	if unowned.Assignee != "seat-a" {
		t.Fatalf("an unowned required-due issue must fall back to the actor, got %q", unowned.Assignee)
	}

	claimed := dueLessTask()
	claimed.Assignee = "seat-b"
	DefaultDueRequiredAssignee(claimed, "seat-a")
	if claimed.Assignee != "seat-b" {
		t.Fatalf("an explicit assignee must win, got %q", claimed.Assignee)
	}

	owned := dueLessTask()
	owned.Owner = "repo-owner"
	DefaultDueRequiredAssignee(owned, "seat-a")
	if owned.Assignee != "" {
		t.Fatalf("an owned issue already has a destination, got assignee %q", owned.Assignee)
	}

	wisp := dueLessTask()
	wisp.Ephemeral = true
	DefaultDueRequiredAssignee(wisp, "seat-a")
	if wisp.Assignee != "" {
		t.Fatalf("an exempt record must not be assigned, got %q", wisp.Assignee)
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

func TestPreparePublicCreateRequestAssignsSeat(t *testing.T) {
	withDueRequired(t, true)
	issue := dueLessTask()
	due := time.Now().Add(72 * time.Hour)
	issue.DueAt = &due
	prepared, err := PreparePublicCreateRequest(publicops.CreateRequest{Actor: "seat-a", Issue: issue}, PublicCreateContext{IssuePrefix: "bd"})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Issue.Assignee != "seat-a" {
		t.Fatalf("a required-due create must carry a destination, got %q", prepared.Issue.Assignee)
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
