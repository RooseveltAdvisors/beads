package main

import (
	"strings"

	"github.com/steveyegge/beads/internal/notify"
)

// currentNotifyActor is the seat that should receive BEADS NOTIFY output:
// --actor, then BEADS_ACTOR (via getActor / resolveConfiguredActor).
func currentNotifyActor() string {
	if a := strings.TrimSpace(getActor()); a != "" {
		return a
	}
	return strings.TrimSpace(resolveConfiguredActor())
}

func isUnassignedNotifySeat(seat string) bool {
	s := strings.ToLower(strings.TrimSpace(seat))
	return s == "" || s == notify.UnassignedSeat
}

// shouldDeliverNotifySeat is the assignee filter: unassigned never notifies.
// Explicit --seat delivers only that seat (still not unassigned).
// --all delivers every assigned seat (clock fan-out to each assignee's instance).
// Otherwise only the current actor's seat is delivered — no cross-seat print.
func shouldDeliverNotifySeat(seat, actor string, all, explicitSeat bool) bool {
	if isUnassignedNotifySeat(seat) {
		return false
	}
	if all {
		return true
	}
	if explicitSeat {
		return true
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(seat), actor)
}
