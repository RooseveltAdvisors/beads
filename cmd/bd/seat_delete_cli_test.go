package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/seats"
)

func runSeatDelete(t *testing.T, args ...string) error {
	t.Helper()
	rootCmd.SetArgs(append([]string{"seat", "delete"}, args...))
	defer rootCmd.SetArgs(nil)
	return rootCmd.Execute()
}

// TestSeatDeleteFreesLegacyNameForMintCLI drives the migration sequence the
// fleet needs: a legacy name-keyed row shadows a minted alias until the dead
// row is deleted, and only then can the uuid row take the name.
func TestSeatDeleteFreesLegacyNameForMintCLI(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := configfile.DefaultConfig()
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_TEST_MODE", "1")
	t.Setenv("BEADS_DIR", beadsDir)
	t.Setenv(seats.StateEnv, "")
	statePath := filepath.Join(t.TempDir(), "agent-state.json")

	// Legacy name-keyed row with the alias the mint wants.
	if err := runSeatRegister(t, "legacy-seat", "--alias", "beads", "--session", "s", "--pane", "w1:p1", "--agent-session", "/tmp/a.jsonl"); err != nil {
		t.Fatalf("legacy register: %v", err)
	}
	// A mint with the same alias is refused while the legacy row holds it.
	err := runSeatRegister(t, "--state", statePath, "--alias", "beads", "--session", "s", "--pane", "w1:p2", "--agent-session", "/tmp/b.jsonl")
	if err == nil {
		t.Fatal("mint with a shadowed alias parsed, want the ambiguity refusal")
	}

	// Delete refuses the live row; unbind first, then delete.
	if err := runSeatDelete(t, "legacy-seat"); err == nil {
		t.Fatal("delete of a live seat parsed, want the liveness refusal")
	}
	if err := rootCmdSetArgsRun(t, "seat", "unbind", "legacy-seat"); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if err := runSeatDelete(t, "legacy-seat"); err != nil {
		t.Fatalf("delete after unbind: %v", err)
	}
	if err := runSeatDelete(t, "legacy-seat"); err == nil {
		t.Fatal("second delete parsed, want unknown seat id")
	}

	// The freed alias now mints.
	if err := runSeatRegister(t, "--state", statePath, "--alias", "beads", "--session", "s", "--pane", "w1:p2", "--agent-session", "/tmp/b.jsonl"); err != nil {
		t.Fatalf("mint after delete: %v", err)
	}
	regRaw, err := os.ReadFile(filepath.Join(beadsDir, "notify", "seats.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reg map[string]seats.Seat
	if err := json.Unmarshal(regRaw, &reg); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg["legacy-seat"]; ok {
		t.Error("legacy row still present after delete")
	}
	if _, ok := reg["beads"]; ok {
		t.Error("name-keyed beads row exists; the mint must be uuid-keyed")
	}
	uuids := 0
	for id, seat := range reg {
		if len(id) == 36 && id[8] == '-' {
			uuids++
			// The in-process flag harness accumulates pflag StringSlice
			// values across runSeatRegister calls; the real CLI parses once
			// per process. Assert the name, not the slice shape.
			hasBeads := false
			for _, a := range seat.Aliases {
				if a == "" || a == "[]" {
					continue
				}
				if a == "beads" {
					hasBeads = true
				} else {
					t.Errorf("minted seat has unexpected alias %q", a)
				}
			}
			if !hasBeads {
				t.Errorf("minted seat aliases = %v, want beads among them", seat.Aliases)
			}
		}
	}
	if uuids != 1 {
		t.Errorf("minted uuid rows = %d, want 1", uuids)
	}
}

func rootCmdSetArgsRun(t *testing.T, args ...string) error {
	t.Helper()
	rootCmd.SetArgs(args)
	defer rootCmd.SetArgs(nil)
	return rootCmd.Execute()
}
