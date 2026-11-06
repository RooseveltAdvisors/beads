//go:build cgo

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNotifyPendingGuidesOnUnregisteredDefaultActor: the default actor comes
// from git user.name, which after a seat retirement is not a registered seat.
// A bare `bd notify pending` must guide to --seat / BEADS_ACTOR instead of
// dumping the raw registry refusal, while an explicit unknown --seat keeps
// the raw refusal (the caller named it, the registry answer applies).
func TestNotifyPendingGuidesOnUnregisteredDefaultActor(t *testing.T) {
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "np")

	// Two fixture conditions from the live store that exposed this:
	// 1. a configured role, so the default actor reaches git user.name
	// 2. a NON-EMPTY registry - seats.Strict() is len(r) > 0, so an empty
	//    registry passes any name through ResolveSeat and the refusal
	//    never fires. One registered seat makes the registry strict.
	if out, err := exec.Command("git", "-C", dir, "config", "beads.role", "maintainer").CombinedOutput(); err != nil {
		t.Fatalf("set beads.role: %v %s", err, out)
	}
	if _, err := bdRunWithFlockRetry(t, bd, dir, "seat", "register", "dummy-seat",
		"--alias", "dummy", "--session", "s", "--pane", "w1:p1",
		"--agent-session", "/tmp/dummy.jsonl"); err != nil {
		t.Fatalf("register a seat to make the registry strict: %v", err)
	}

	out, err := bdRunWithFlockRetry(t, bd, dir, "notify", "pending")
	if err == nil {
		t.Fatalf("unregistered default actor must refuse, got success:\n%s", out)
	}
	combined := string(out)
	if !strings.Contains(combined, "is not registered") || !strings.Contains(combined, "--seat") {
		t.Fatalf("want guided hint naming --seat and the refusal reason, got:\n%s", combined)
	}

	// Explicit unknown seat keeps the raw registry refusal.
	out2, err := bdRunWithFlockRetry(t, bd, dir, "notify", "pending", "--seat", "ghost-seat")
	if err == nil {
		t.Fatalf("unknown explicit seat must refuse, got success:\n%s", out2)
	}
	if !strings.Contains(string(out2), "unknown seat") {
		t.Fatalf("explicit seat keeps the raw registry refusal, got:\n%s", out2)
	}
}
