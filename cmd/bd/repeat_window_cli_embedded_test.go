//go:build cgo

package main

import (
	"strings"
	"testing"
)

// The quiet window had a product-dead path once: the parse worked and the
// storage layer honored it, but no CLI command wrote the column, so the
// documented usage did nothing. This walks the exact usage a future agent
// will copy: create with --window, read it back, update it, read it back.
func TestRepeatWindowRoundTripsThroughTheCLI(t *testing.T) {
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "rw")

	issue := bdCreate(t, bd, dir, "customer thread wake", "--repeat", "*/10 9-19 * * *", "--window", "09:00-20:00 America/New_York")
	if issue.RepeatWindow != "09:00-20:00 America/New_York" {
		t.Fatalf("create --window: RepeatWindow = %q, want the window stored", issue.RepeatWindow)
	}

	if out, err := bdRunWithFlockRetry(t, bd, dir, "update", issue.ID, "--window", "08:00-18:00 Europe/London"); err != nil {
		t.Fatalf("bd update --window failed: %v\n%s", err, out)
	}
	got := bdShow(t, bd, dir, issue.ID)
	if got.RepeatWindow != "08:00-18:00 Europe/London" {
		t.Fatalf("update --window: RepeatWindow = %q, want the new window", got.RepeatWindow)
	}

	// An unparseable window is refused, not stored.
	if out, err := bdRunWithFlockRetry(t, bd, dir, "update", issue.ID, "--window", "nonsense"); err == nil {
		t.Fatalf("bd update --window nonsense = nil error, want refusal\n%s", out)
	}
	// Empty clears.
	if out, err := bdRunWithFlockRetry(t, bd, dir, "update", issue.ID, "--window", ""); err != nil {
		t.Fatalf("bd update --window \"\" failed: %v\n%s", err, out)
	}
	cleared := bdShow(t, bd, dir, issue.ID)
	if cleared.RepeatWindow != "" {
		t.Fatalf("update --window \"\": RepeatWindow = %q, want empty", cleared.RepeatWindow)
	}
	_ = strings.TrimSpace
}
