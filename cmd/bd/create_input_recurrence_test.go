package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestGatherCreateInputCarriesRecurrenceFlags pins the input seam both create
// transports share: the recurrence flags must land in createInput.metadata so
// the proxied-server route persists them instead of silently dropping them.
func TestGatherCreateInputCarriesRecurrenceFlags(t *testing.T) {
	t.Parallel()
	cmd := newCreateFlagsCommand(t,
		"--assignee", "jr-voice",
		"--repeat", "0 9 * * *",
		"--recurrence-start", "2026-09-08",
		"--recurrence-tz", "America/New_York",
	)
	in, err := gatherCreateInput(cmd, []string{"Publish daily jonroosevelt.com blog post"})
	if err != nil {
		t.Fatalf("gatherCreateInput: %v", err)
	}
	recurrence, err := types.ParseRecurrence(in.metadata, in.assignee)
	if err != nil {
		t.Fatalf("gathered metadata does not parse recurring: %v (%s)", err, in.metadata)
	}
	if recurrence == nil {
		t.Fatalf("gathered metadata %s does not form a recurrence, want the recurrence contract carried", in.metadata)
	}
	if recurrence.Schedule != "0 9 * * *" || recurrence.Start != "2026-09-08" || recurrence.Timezone != "America/New_York" {
		t.Fatalf("gathered recurrence = %+v", recurrence)
	}
}

func TestGatherCreateInputRefusesInvalidRecurrenceSchedule(t *testing.T) {
	t.Parallel()
	cmd := newCreateFlagsCommand(t,
		"--assignee", "jr-voice",
		"--repeat", "61 9 * * *",
		"--recurrence-start", "2026-09-08",
		"--recurrence-tz", "UTC",
	)
	_, err := gatherCreateInput(cmd, []string{"Broken schedule"})
	// HandleError prints the refusal ("invalid recurrence schedule ...") to
	// stderr and returns the process exit error; the observable contract here
	// is the refusal itself.
	if err == nil {
		t.Fatal("gatherCreateInput() = nil error, want invalid recurrence schedule refusal")
	}
}

func TestGatherCreateInputStrictValidatesRecurrenceKeysInMetadata(t *testing.T) {
	t.Parallel()
	cmd := newCreateFlagsCommand(t,
		"--metadata", `{"repeat":"weekly"}`,
	)
	_, err := gatherCreateInput(cmd, []string{"Legacy shaped metadata"})
	if err == nil {
		t.Fatal("gatherCreateInput() = nil error, want strict recurrence refusal for recurrence keys in --metadata")
	}
}

func TestGatherCreateInputRejectsRecurrenceFlagsForMarkdownAndGraph(t *testing.T) {
	t.Parallel()
	markdown := filepath.Join(t.TempDir(), "issues.md")
	if err := os.WriteFile(markdown, []byte("---\ntitle: From markdown\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newCreateFlagsCommand(t, "--file", markdown, "--repeat", "daily")
	if _, err := gatherCreateInput(cmd, nil); err == nil {
		t.Fatal("markdown route accepted --repeat, want refusal")
	}

	graph := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(graph, []byte(`{"nodes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = newCreateFlagsCommand(t, "--graph", graph, "--recurrence-tz", "UTC")
	if _, err := gatherCreateInput(cmd, nil); err == nil {
		t.Fatal("graph route accepted --recurrence-tz, want refusal")
	}
}
