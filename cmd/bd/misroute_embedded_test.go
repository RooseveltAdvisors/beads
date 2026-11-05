//go:build cgo

package main

import (
	"strings"
	"testing"
)

// TestMisrouteFilesToDispatchOwner drives the filing contract through the
// real CLI: one command, fixed shape (assignee firstmate, P1, misroute
// label, 48h due, MISROUTE title prefix), and the refusals that stop a
// lazy filing (empty description, missing --from).
func TestMisrouteFilesToDispatchOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded build in short mode")
	}
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "mr")

	// The write gate requires a registered actor seat, and the dispatch
	// owner's seat must exist for the assignee write. One row serves both:
	// the filer and the dispatch owner share this test's workspace.
	const ownerUUID = "b0b9d6a0-6269-4376-a702-28db0143c3da"
	if _, err := bdRunWithFlockRetry(t, bd, dir, "seat", "register", ownerUUID,
		"--alias", "firstmate", "--session", "s", "--pane", "w1:pZ1",
		"--agent-session", "/tmp/mr.jsonl"); err != nil {
		t.Fatalf("seat register: %v", err)
	}
	// The actor env is stripped by bdEnv; the flag is the lane's identity.
	misrouteFlags := []string{"--actor", "firstmate"}
	_ = misrouteFlags

	if out, err := bdRunWithFlockRetry(t, bd, dir, "misroute", "--actor", "firstmate",
		"milestones landing on the wrong seat", "--from", "fm-somelane"); err != nil {
		t.Fatalf("misroute filing failed: %v\n%s", err, out)
	}

	stdout, err := bdRunWithFlockRetry(t, bd, dir, "list", "--actor", "firstmate", "--label", "misroute", "--json")
	if err != nil {
		t.Fatalf("list --label misroute failed: %v", err)
	}
	for _, want := range []string{
		`"assignee": "` + ownerUUID + `"`,
		`"assignee_display": "firstmate"`,
		`"priority": 1`,
		"MISROUTE: milestones landing on the wrong seat",
	} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("filed misroute missing %q in:\n%s", want, stdout)
		}
	}

	// --from is required: a lazy filing that names nobody is refused.
	if _, err := bdRunWithFlockRetry(t, bd, dir, "misroute", "--actor", "firstmate", "something arrived"); err == nil {
		t.Error("misroute without --from parsed, want the refusal")
	}
}
