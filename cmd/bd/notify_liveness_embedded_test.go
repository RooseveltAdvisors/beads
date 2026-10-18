//go:build cgo

package main

import (
	"os"
	"strings"
	"testing"
)

func TestEmbeddedNotifyShowsOnlyOwnLiveWork(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "nl")
	closed := bdCreate(t, bd, dir, "Closed after due", "--assignee", "alice", "--due", pastDue())
	held := bdCreate(t, bd, dir, "Held due", "--assignee", "alice", "--due", pastDue(), "--labels", heldNotifyLabel)
	live := bdCreate(t, bd, dir, "Live due", "--assignee", "alice", "--due", pastDue())
	unassigned := bdCreate(t, bd, dir, "Unassigned due", "--due", pastDue())
	self := bdCreate(t, bd, dir, "Self comment", "--assignee", "firstmate")
	bdDueSweep(t, bd, dir)
	bdDueSweep(t, bd, dir)
	bdClose(t, bd, dir, closed.ID, "--force")

	run := func(args ...string) string {
		t.Helper()
		out, err := bdRunWithFlockRetry(t, bd, dir, args...)
		if err != nil {
			t.Fatalf("bd %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	pending := run("notify", "pending", "--seat", "alice")
	if n := strings.Count(pending, live.ID); n != 1 {
		t.Fatalf("live due bead pending %d times, want 1:\n%s", n, pending)
	}
	for _, id := range []string{closed.ID, held.ID, unassigned.ID} {
		if strings.Contains(pending, id) {
			t.Fatalf("pending for alice lists non-live or foreign bead %s:\n%s", id, pending)
		}
	}
	if all := run("notify", "pending"); strings.Contains(all, unassigned.ID) {
		t.Fatalf("unassigned due bead was enqueued:\n%s", all)
	}

	run("comment", closed.ID, "closed comment", "--actor", "bob")
	run("comment", self.ID, "self comment", "--actor", "firstmate")
	if out := run("notify", "pending", "--seat", "firstmate"); strings.Contains(out, self.ID) {
		t.Fatalf("self comment queued a ping:\n%s", out)
	}

	run("label", "remove", held.ID, heldNotifyLabel)
	if pending := run("notify", "pending", "--seat", "alice"); !strings.Contains(pending, held.ID) {
		t.Fatalf("bead not pending after held label removal:\n%s", pending)
	}

	drained := run("notify", "drain", "--seat", "alice", "--print")
	if !strings.Contains(drained, held.ID) || !strings.Contains(drained, live.ID) {
		t.Fatalf("drain did not deliver live rows:\n%s", drained)
	}
	if strings.Contains(drained, closed.ID) {
		t.Fatalf("drain delivered a closed bead:\n%s", drained)
	}
	if pending := run("notify", "pending", "--seat", "alice"); strings.Contains(pending, closed.ID) {
		t.Fatalf("closed row remained pending after drain:\n%s", pending)
	}
}
