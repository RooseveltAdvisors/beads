//go:build cgo

package main

import (
	"strings"
	"testing"
)

// The queue ceilings exist because a backlog reached 2,000+ beads with nothing
// saying stop: intake outran closing for weeks. These tests pin the two
// refusals and their bypass, so the stop cannot quietly disappear.
func TestQueueCap_RefusesPastTheCeiling(t *testing.T) {
	t.Setenv("BD_CREATE_MAX_OPEN_PER_ASSIGNEE", "2")
	t.Setenv("BD_CREATE_MAX_OPEN_TOTAL", "0") // isolate the per-assignee ceiling
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "qc")

	if got := bdCreate(t, bd, dir, "queue cap first task").ID; got == "" {
		t.Fatal("first create refused")
	}
	if got := bdCreate(t, bd, dir, "queue cap second task").ID; got == "" {
		t.Fatal("second create refused")
	}

	out := bdCreateFail(t, bd, dir, "queue cap third task")
	if !strings.Contains(out, "queue cap:") || !strings.Contains(out, "create.max-open-per-assignee=2") {
		t.Fatalf("expected the per-assignee ceiling message, got: %s", out)
	}
	if !strings.Contains(out, "bd config set create.max-open-per-assignee") || !strings.Contains(out, "0 turns it off") {
		t.Fatalf("expected the raise/turn-off guidance in the error, got: %s", out)
	}

	// The documented bypass is the ceiling itself: raising it (or 0 = off)
	// is the deliberate act, not a flag on one call.
	t.Setenv("BD_CREATE_MAX_OPEN_PER_ASSIGNEE", "3")
	if got := bdCreate(t, bd, dir, "queue cap third task").ID; got == "" {
		t.Fatal("create refused after the ceiling was raised")
	}
}

func TestQueueCap_TotalCeiling(t *testing.T) {
	t.Setenv("BD_CREATE_MAX_OPEN_PER_ASSIGNEE", "0") // isolate the total ceiling
	t.Setenv("BD_CREATE_MAX_OPEN_TOTAL", "1")
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "qt")

	if got := bdCreate(t, bd, dir, "the one task").ID; got == "" {
		t.Fatal("first create refused")
	}
	out := bdCreateFail(t, bd, dir, "one task too many")
	if !strings.Contains(out, "create.max-open-total=1") {
		t.Fatalf("expected the total ceiling message, got: %s", out)
	}
	if !strings.Contains(out, "close what is done or on ice") {
		t.Fatalf("expected the triage guidance, got: %s", out)
	}
}

// The ceilings are off only when a workspace turns them off: 0 means off, and
// a fresh workspace runs with the defaults rather than unbounded.
func TestQueueCap_ZeroMeansOff(t *testing.T) {
	t.Setenv("BD_CREATE_MAX_OPEN_PER_ASSIGNEE", "0")
	t.Setenv("BD_CREATE_MAX_OPEN_TOTAL", "0")
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "qz")

	for i := 0; i < 3; i++ {
		if got := bdCreate(t, bd, dir, "unbounded queue task").ID; got == "" {
			t.Fatalf("create %d refused with both ceilings at 0", i+1)
		}
	}
}
