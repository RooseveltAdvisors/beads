package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/seats"
)

// seatRegisterCmd finds the live register subcommand for in-process runs.
func seatRegisterCmd(t *testing.T) *cobra.Command {
	t.Helper()
	for _, c := range seatCmd.Commands() {
		if c.Name() == "register" {
			return c
		}
	}
	t.Fatal("seat register command not found")
	return nil
}

// resetSeatRegisterFlags clears Changed bits and values so one test process
// can run the singleton command several times.
func resetSeatRegisterFlags(t *testing.T) {
	cmd := seatRegisterCmd(t)
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		if err := f.Value.Set(f.DefValue); err != nil {
			t.Fatalf("reset flag %s: %v", f.Name, err)
		}
	})
}

func runSeatRegister(t *testing.T, args ...string) error {
	t.Helper()
	resetSeatRegisterFlags(t)
	rootCmd.SetArgs(append([]string{"seat", "register"}, args...))
	defer rootCmd.SetArgs(nil)
	return rootCmd.Execute()
}

// TestSeatRegisterMintReregisterForeignShellCLI drives the acceptance journey
// through the real command surface: mint with durable state, restart into a
// new pane and re-register under the same uuid, then a foreign shell that
// knows the uuid but holds no durable state is refused.
func TestSeatRegisterMintReregisterForeignShellCLI(t *testing.T) {
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

	// Mint: no seat id, no existing state.
	if err := runSeatRegister(t, "--state", statePath, "--alias", "worker", "--session", "wiseman", "--pane", "w1:p1"); err != nil {
		t.Fatalf("mint: %v", err)
	}
	st, err := seats.ReadState(statePath)
	if err != nil {
		t.Fatalf("durable state not written: %v", err)
	}
	r, err := seats.Load(filepath.Join(beadsDir, "notify"))
	if err != nil {
		t.Fatal(err)
	}
	seat, ok := r[st.AgentUUID]
	if !ok || len(r) != 1 {
		t.Fatalf("registry = %v, want exactly the minted uuid %s", r, st.AgentUUID)
	}
	if seat.Endpoint.PaneID != "w1:p1" || seat.TokenSHA256 == "" {
		t.Fatalf("minted seat = %+v", seat)
	}

	// Restart into a NEW pane: re-register from the durable state alone.
	if err := runSeatRegister(t, "--state", statePath, "--session", "wiseman", "--pane", "w1:p2"); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	r, err = seats.Load(filepath.Join(beadsDir, "notify"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 || r[st.AgentUUID].Endpoint.PaneID != "w1:p2" {
		t.Fatalf("re-register churned identity or endpoint: %v", r)
	}

	// Foreign shell: knows the uuid, holds no token.
	if err := runSeatRegister(t, st.AgentUUID, "--session", "wiseman", "--pane", "w1:pEVIL"); err == nil {
		t.Fatal("foreign shell registered under the minted uuid")
	}
	r, err = seats.Load(filepath.Join(beadsDir, "notify"))
	if err != nil {
		t.Fatal(err)
	}
	if r[st.AgentUUID].Endpoint.PaneID != "w1:p2" {
		t.Fatalf("foreign shell moved the endpoint: %+v", r[st.AgentUUID].Endpoint)
	}

	// A corrupt durable state refuses rather than silently re-minting.
	corrupt := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSeatRegister(t, "--state", corrupt, "--alias", "worker", "--session", "wiseman", "--pane", "w1:p9"); err == nil {
		t.Fatal("corrupt durable state silently re-minted")
	}
	if r, err := seats.Load(filepath.Join(beadsDir, "notify")); err != nil || len(r) != 1 {
		t.Fatalf("corrupt state changed the registry: %v %v", r, err)
	}
}
