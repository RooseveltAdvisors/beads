package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/types"
)

// quickDueLadderDays is the days-from-now a quick-captured issue is due, by
// priority: the more urgent the work, the sooner the deadline it inherits.
// Indexed by priority 0-4.
var quickDueLadderDays = [5]int{1, 3, 7, 14, 30}

// resolveQuickDue settles `bd q`'s due date: an explicit --due wins, otherwise
// the priority ladder supplies one so quick capture stays a one-liner under a
// workspace that requires due dates.
//
// The ladder only fires when `due.required` is on. With the invariant off this
// is upstream `bd q`, which mints no due date at all, and stays that way.
func resolveQuickDue(cmd *cobra.Command, issue *types.Issue) error {
	dueStr, _ := cmd.Flags().GetString("due")
	if dueStr != "" {
		t, err := timeparsing.ParseRelativeTime(dueStr, time.Now())
		if err != nil {
			return HandleError("invalid --due format %q. Examples: +6h, tomorrow, next monday, 2025-01-15", dueStr)
		}
		issue.DueAt = &t
		return nil
	}
	if !issueops.DueRequiredEnabled() || issueops.DueRequiredExempt(issue) {
		return nil
	}
	priority := issue.Priority
	if priority < 0 || priority >= len(quickDueLadderDays) {
		priority = len(quickDueLadderDays) - 1
	}
	due := time.Now().AddDate(0, 0, quickDueLadderDays[priority])
	issue.DueAt = &due
	return nil
}

// echoQuickDue reports the due date the ladder resolved, on STDERR: `bd q`
// prints the new issue's ID and nothing else on stdout, because `ID=$(bd q …)`
// is its whole reason for existing.
func echoQuickDue(issue *types.Issue) {
	if issue.DueAt == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "due %s (P%d)\n", issue.DueAt.Format(time.RFC3339), issue.Priority)
}
