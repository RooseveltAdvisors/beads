package issueops

import "github.com/steveyegge/beads/internal/seats"

// PriorityDecayFloor is the rung decay-on-attention settles at. Escalation
// raises a chronic misser one rung at the miss threshold (escalatedPriority);
// before decay existed nothing ever came down, which left 92% of a live
// house store at P0/P1 and the priority field unable to rank work (audit
// 2026-11-01, captain order: decay-on-attention). Attention walks a bead back
// toward the normal band, one rung per real interaction, and stops at P2: a
// worked bead returns to baseline, never to the bottom.
const PriorityDecayFloor = 2

// DecayPriorityOnAttention gives one priority rung back on a real
// interaction. It reports the new value and whether a decay applies; a bead
// at or below the floor (numerically at or above P2) is untouched.
func DecayPriorityOnAttention(current int) (int, bool) {
	if current >= PriorityDecayFloor {
		return current, false
	}
	return current + 1, true
}

// ApplyDecayOnAttention folds the decay into an update map. It belongs at
// the INTERACTION layer (the bd update / bd comment commands), never inside
// issueops: integrations (ADO sync, importers, federation) write through the
// same storage seam, and a sync is machinery, not attention - decaying there
// would mutate external state on every pull. An explicit priority write is
// the operator's word and is never decayed in the same breath; machine actors
// (the sweep, defer wakes) are not attention; everything else gives one rung
// back.
func ApplyDecayOnAttention(updates map[string]interface{}, currentPriority int, actor string) {
	if _, explicit := updates["priority"]; explicit {
		return
	}
	if MachineActorForDecay(actor) {
		return
	}
	if next, decayed := DecayPriorityOnAttention(currentPriority); decayed {
		updates["priority"] = next
	}
}

// MachineActorForDecay reports whether actor is one of the product's own
// automation identities, whose writes are machinery, not attention.
func MachineActorForDecay(actor string) bool {
	return seats.MachineActor(actor) != ""
}
