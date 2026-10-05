package issueops

import (
	"fmt"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

// Recurrence close guard (house, captain 2026-10-05).
//
// Closing a recurring bead files a NEW row for its successor
// (SpawnRecurrenceInTx), so a cadence's row cost is one row per fire: seat
// billing-learn measured 144 rows/day at */10, from a bead whose description
// said "close this bead so the next instance spawns" while the due-notify
// playbook told the same agent to `bd close <id>`.
//
// Prose cannot hold that line against an agent reading two contradictory
// instructions, so the close itself refuses: a heartbeat below the cadence
// floor stays open, and the due sweep re-fires it with no row cost.
//
// Deliberate escapes only:
//   - end the series first: bd update <id> --repeat "" --reason "..."
//   - workspace policy:    bd config set recurrence.close_guard false
//
// `bd close --force` does not bypass this gate; force covers blockers and
// children, not a cadence that would file identical history rows.
const (
	// RecurrenceCloseGuardKey turns the guard on. House default true.
	RecurrenceCloseGuardKey = "recurrence.close_guard"
)

// RecurrenceCloseGuardEnabled reports whether this workspace gates spawns by
// cadence.
func RecurrenceCloseGuardEnabled() bool {
	return config.GetBool(RecurrenceCloseGuardKey)
}

// ValidateRecurrenceSpawn refuses a successor that would fire faster than the
// cadence floor. SpawnRecurrenceInTx calls it, so every path that can end a
// recurring bead meets it in one place: bd close, the store close,
// update --status closed (the crossing spawn), and molecule steps.
func ValidateRecurrenceSpawn(issue *types.Issue) error {
	if !RecurrenceCloseGuardEnabled() || issue == nil || !issue.IsRecurring() {
		return nil
	}
	return fmt.Errorf(
		"recurrence.close_guard: %s is a recurring bead, and closing it files a NEW row for its successor. "+
			"Work on the SAME bead: comment your result and leave it open - the due sweep re-dates it and re-fires it. "+
			"To end the series: bd update "+issue.ID+" --repeat \"\", then close. "+
			"bd close --force does not bypass this gate; a workspace can opt out with: bd config set %s false",
		issue.ID, RecurrenceCloseGuardKey)
}

// RecurrenceCloseHint is the generated prompt footer for a recurring bead.
// The due-notify playbook says "close it in beads", which is wrong advice for
// a bead the tool will refuse to close, so the rule is generated from the row
// at delivery time instead of being authored into it.
func RecurrenceCloseHint(issue *types.Issue) string {
	if !RecurrenceCloseGuardEnabled() || issue == nil || !issue.IsRecurring() {
		return ""
	}
	return fmt.Sprintf(
		"Recurrence note (%s): work on THIS bead - comment your result and leave it open. "+
			"bd refuses to close it (recurrence.close_guard) because closing would file a new row; "+
			"the due sweep re-dates it and re-fires it. To end the series: bd update "+issue.ID+" --repeat \"\", then close.",
		issue.ID)
}
