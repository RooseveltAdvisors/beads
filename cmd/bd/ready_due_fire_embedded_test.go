//go:build cgo

package main

import (
	"os"
	"testing"
	"time"
)

// TestReadyReadFiresPastDueRecurringBead pins the PULL path that the notify
// clock retirement (wiseman-opvu) stands on: `bd due sweep` is a contrib
// timer wrapping lazy core sweeps, so the read an agent already runs must do
// the firing the timer used to do. A --repeat bead whose due date has passed
// must (1) appear in `bd ready` and (2) FIRE on that very read - due_at
// re-armed past now, due_missed = 1 - not merely be listed.
//
// If this test fails, retiring the clock would strand due beads silently:
// they would still show up (open is open) while nobody ever advances the
// recurrence, increments the miss, or escalates the streak. The defer-wake
// twins (defer_wake_embedded_test.go, be-vbhpf) pin the same contract for
// the defer arm; this is the due arm's twin at the CLI.
func TestReadyReadFiresPastDueRecurringBead(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "rvf")
	issue := bdCreate(t, bd, dir, "Recurring pull probe", "--type", "task",
		"--due", pastDue(), "--repeat", "+1d")

	if issue.DueMissed != 0 {
		t.Fatalf("due_missed = %d at creation, want 0 (the read must be what fires)", issue.DueMissed)
	}

	// The pull read - what an agent runs, no clock involved.
	ids := wakeReadyIDs(t, bd, dir)
	if !ids[issue.ID] {
		t.Errorf("%s is missing from bd ready", issue.ID)
	}

	after := bdShow(t, bd, dir, issue.ID)
	if after.DueAt == nil || !after.DueAt.After(time.Now().UTC()) {
		t.Errorf("due_at = %v, want a future date: the ready read must fire the miss and re-arm the recurrence", after.DueAt)
	}
	if after.DueMissed != 1 {
		t.Errorf("due_missed = %d, want 1 (the ready read fired the miss exactly once)", after.DueMissed)
	}
	if after.DueSource != "repeat" {
		t.Errorf("due_source = %q, want repeat (the pattern computed the new date)", after.DueSource)
	}

	// The reschedule is the idempotency mechanism: a second read with no time
	// passed must not re-fire, or every ready read re-fires forever.
	if ids := wakeReadyIDs(t, bd, dir); !ids[issue.ID] {
		t.Errorf("%s dropped out of bd ready after the fire (still open work)", issue.ID)
	}
	again := bdShow(t, bd, dir, issue.ID)
	if again.DueMissed != 1 {
		t.Errorf("due_missed = %d after a second read, want 1 (no re-fire without time passing)", again.DueMissed)
	}
}
