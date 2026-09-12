package main

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// withDueRequired turns the mandatory-due invariant on or off for one test and
// restores the process-wide config singleton afterwards. Not parallel-safe.
func withDueRequired(t *testing.T, enabled bool) {
	t.Helper()
	t.Chdir(t.TempDir())
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	config.Set(issueops.DueRequiredKey, enabled)
	t.Cleanup(config.ResetForTesting)
}

// quickFlags builds a stand-in for quickCmd carrying only the flags
// resolveQuickDue and requireForceNoDue read.
func quickFlags(t *testing.T, set map[string]string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "q"}
	cmd.Flags().String("due", "", "")
	cmd.Flags().Bool("force-no-due", false, "")
	cmd.Flags().String("reason", "", "")
	for name, value := range set {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}
	return cmd
}

func TestResolveQuickDueLadder(t *testing.T) {
	withDueRequired(t, true)
	// The ladder Jon's fleet runs on: the more urgent the priority, the sooner
	// quick capture's implied deadline.
	wantDays := map[int]int{0: 1, 1: 3, 2: 7, 3: 14, 4: 30}
	for priority, days := range wantDays {
		issue := &types.Issue{Title: "ladder", IssueType: types.TypeTask, Priority: priority}
		if err := resolveQuickDue(quickFlags(t, nil), issue); err != nil {
			t.Fatalf("P%d: %v", priority, err)
		}
		if issue.DueAt == nil {
			t.Fatalf("P%d: ladder produced no due date", priority)
		}
		want := time.Now().AddDate(0, 0, days)
		if delta := issue.DueAt.Sub(want); delta > time.Minute || delta < -time.Minute {
			t.Fatalf("P%d: due %s, want ~%s (+%dd)", priority, issue.DueAt, want, days)
		}
	}
}

func TestResolveQuickDueExplicitFlagWins(t *testing.T) {
	withDueRequired(t, true)
	issue := &types.Issue{Title: "explicit", IssueType: types.TypeTask, Priority: 0}
	if err := resolveQuickDue(quickFlags(t, map[string]string{"due": "+90d"}), issue); err != nil {
		t.Fatal(err)
	}
	if issue.DueAt == nil || issue.DueAt.Before(time.Now().AddDate(0, 0, 80)) {
		t.Fatalf("an explicit --due must beat the P0 ladder rung, got %v", issue.DueAt)
	}
}

func TestResolveQuickDueIsInertWhenInvariantOff(t *testing.T) {
	withDueRequired(t, false)
	issue := &types.Issue{Title: "upstream", IssueType: types.TypeTask, Priority: 1}
	if err := resolveQuickDue(quickFlags(t, nil), issue); err != nil {
		t.Fatal(err)
	}
	if issue.DueAt != nil {
		t.Fatalf("with the invariant off bd q must mint no due date, got %v", issue.DueAt)
	}
}

func TestResolveQuickDueSkipsExemptRecords(t *testing.T) {
	withDueRequired(t, true)
	issue := &types.Issue{Title: "audit", IssueType: types.TypeEvent, Priority: 2}
	if err := resolveQuickDue(quickFlags(t, nil), issue); err != nil {
		t.Fatal(err)
	}
	if issue.DueAt != nil {
		t.Fatalf("an exempt record must not get a ladder due date, got %v", issue.DueAt)
	}
}

func TestRequireForceNoDue(t *testing.T) {
	withDueRequired(t, true)

	if err := requireForceNoDue(quickFlags(t, nil)); err == nil ||
		!strings.Contains(err.Error(), "--force-no-due") {
		t.Fatalf("clearing a due date must demand --force-no-due, got %v", err)
	}
	if err := requireForceNoDue(quickFlags(t, map[string]string{"force-no-due": "true"})); err == nil ||
		!strings.Contains(err.Error(), "--reason") {
		t.Fatalf("--force-no-due must demand a reason, got %v", err)
	}
	if err := requireForceNoDue(quickFlags(t, map[string]string{"force-no-due": "true", "reason": "   "})); err == nil {
		t.Fatalf("a blank reason is not a reason")
	}
	if err := requireForceNoDue(quickFlags(t, map[string]string{"force-no-due": "true", "reason": "superseded by bd-x"})); err != nil {
		t.Fatalf("a forced, reasoned clear must be permitted, got %v", err)
	}
}

func TestRequireForceNoDueIsInertWhenInvariantOff(t *testing.T) {
	withDueRequired(t, false)
	if err := requireForceNoDue(quickFlags(t, nil)); err != nil {
		t.Fatalf("with the invariant off, clearing a due date is unchanged, got %v", err)
	}
}
