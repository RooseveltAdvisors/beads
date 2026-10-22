package seats

import "strings"

// Machine actors are the product's own automation identities: the actor a
// due-date trigger records, the actor a spawned successor bead carries, and
// the actor a defer wake records. They are not seats - no endpoint, no
// liveness - so the registry admits them by exact name only, never by
// pattern. This is the single source of truth for those names; storage code
// aliases its constants from here so the sets cannot drift.
const (
	MachineActorDueTrigger      = "bd-due-trigger"
	MachineActorRecurrenceSpawn = "bd-recurrence"
	MachineActorDeferWake       = "bd-defer-wake"
)

// machineActors lists the canonical spellings in one place.
var machineActors = []string{
	MachineActorDueTrigger,
	MachineActorRecurrenceSpawn,
	MachineActorDeferWake,
}

// MachineActor returns the canonical spelling of a machine actor, or "" when
// name is not one. Matching is case-folded exact, the same rule seat IDs and
// aliases use. There is no prefix, suffix, or pattern admission.
func MachineActor(name string) string {
	for _, actor := range machineActors {
		if strings.EqualFold(name, actor) {
			return actor
		}
	}
	return ""
}
