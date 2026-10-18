//go:build cgo

package main

import (
	"os"
	"strings"
	"testing"
)

func TestEmbeddedNotifyDrainSkipsClosedAndCaptainHeldDueBeads(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "nl")
	closed := bdCreate(t, bd, dir, "Closed after due", "--assignee", "alice", "--due", pastDue())
	held := bdCreate(t, bd, dir, "Captain-held due", "--assignee", "alice", "--due", pastDue(),
		"--metadata", `{"hold_kind":"captain"}`)
	self := bdCreate(t, bd, dir, "Self comment", "--assignee", "firstmate")
	live := bdCreate(t, bd, dir, "Live due", "--assignee", "alice", "--due", pastDue())
	unassigned := bdCreate(t, bd, dir, "Unassigned due", "--due", pastDue())
	bdDueSweep(t, bd, dir)
	bdDueSweep(t, bd, dir)

	pending, err := bdRunWithFlockRetry(t, bd, dir, "notify", "pending", "--seat", "alice")
	if err != nil {
		t.Fatalf("notify pending failed: %v\n%s", err, pending)
	}
	if n := strings.Count(string(pending), live.ID); n != 1 {
		t.Fatalf("live due bead pending %d times, want 1:\n%s", n, pending)
	}
	if strings.Contains(string(pending), unassigned.ID) {
		t.Fatalf("unassigned due bead leaked into alice's pending:\n%s", pending)
	}
	all, err := bdRunWithFlockRetry(t, bd, dir, "notify", "pending")
	if err != nil {
		t.Fatalf("notify pending (all) failed: %v\n%s", err, all)
	}
	if strings.Contains(string(all), unassigned.ID) {
		t.Fatalf("unassigned due bead was enqueued:\n%s", all)
	}

	bdClose(t, bd, dir, closed.ID, "--force")
	if out, err := bdRunWithFlockRetry(t, bd, dir, "comment", closed.ID, "closed comment", "--actor", "alice"); err != nil {
		t.Fatalf("closed comment failed: %v\n%s", err, out)
	}
	if out, err := bdRunWithFlockRetry(t, bd, dir, "comment", self.ID, "self comment", "--actor", "firstmate"); err != nil {
		t.Fatalf("self comment failed: %v\n%s", err, out)
	}

	out, err := bdRunWithFlockRetry(t, bd, dir, "notify", "drain", "--seat", "alice", "--print")
	if err != nil {
		t.Fatalf("notify drain failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), closed.ID) || strings.Contains(string(out), held.ID) {
		t.Fatalf("notify drain delivered suppressed rows:\n%s", out)
	}

	pending, err = bdRunWithFlockRetry(t, bd, dir, "notify", "pending", "--seat", "alice")
	if err != nil {
		t.Fatalf("notify pending failed: %v\n%s", err, pending)
	}
	if strings.Contains(string(pending), closed.ID) || strings.Contains(string(pending), held.ID) {
		t.Fatalf("suppressed rows remained pending:\n%s", pending)
	}
	pending, err = bdRunWithFlockRetry(t, bd, dir, "notify", "pending", "--seat", "firstmate")
	if err != nil {
		t.Fatalf("self pending failed: %v\n%s", err, pending)
	}
	if strings.Contains(string(pending), self.ID) {
		t.Fatalf("self comment queued a ping:\n%s", pending)
	}
}
