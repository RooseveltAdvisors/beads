package notify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/seats"
	"time"
)

func writeLegacyPins(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PinsFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoadPinsFallsBackToLegacyPinsWithoutSeatsFile pins the backward
// compatible read path: an upgraded workspace that has a populated pins.json
// and NO seats.json still yields working pins, instead of degrading to an
// exit-5 no-pin hold on every drain.
func TestLoadPinsFallsBackToLegacyPinsWithoutSeatsFile(t *testing.T) {
	dir := t.TempDir()
	writeLegacyPins(t, dir, `{
		"wiseman": {"session": "s1", "pane_id": "w1:pJ"},
		"gpu-ops": {"agent_session_id": "agent-77"}
	}`)

	pins, err := LoadPins(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pins) != 2 {
		t.Fatalf("pins = %#v, want the 2 legacy entries", pins)
	}
	wiseman := pins["wiseman"]
	if wiseman.Session != "s1" || wiseman.PaneID != "w1:pJ" || !wiseman.Valid() {
		t.Fatalf("wiseman pin = %#v, want the legacy session+pane target", wiseman)
	}
	if pins["gpu-ops"].AgentSessionID != "agent-77" || !pins["gpu-ops"].Valid() {
		t.Fatalf("gpu-ops pin = %#v, want the legacy agent-session target", pins["gpu-ops"])
	}
	if _, err := os.Stat(filepath.Join(dir, seats.File)); !os.IsNotExist(err) {
		t.Fatalf("fallback must not create seats.json: stat err = %v", err)
	}
}

// TestImportPinsReportsEveryPinAndIsIdempotent pins the one-time migration:
// every pin is reported on every pass (mapped / already-present /
// unresolved), valid pins land as seats with their old name kept as the
// alias, invalid or endpoint-less pins are never invented into seats, and a
// second run changes nothing.
func TestImportPinsReportsEveryPinAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeLegacyPins(t, dir, `{
		"wiseman": {"session": "s1", "pane_id": "w1:pJ", "agent_session_id": "asid-1"},
		"bad key": {"session": "s1", "pane_id": "w1:zz"},
		"no-endpoint": {"session": "s1"}
	}`)

	rows, err := ImportPins(dir, time.Unix(1700000000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]PinsImportRow{}
	for _, row := range rows {
		actions[row.PinKey] = row
	}
	if len(rows) != 3 {
		t.Fatalf("first run reported %d rows (%#v), want all 3 pins", len(rows), rows)
	}
	if got := actions["wiseman"]; got.Action != "mapped" || got.SeatID != "wiseman" {
		t.Fatalf("wiseman row = %#v, want mapped to wiseman", got)
	}
	if got := actions["bad key"]; got.Action != "unresolved" || got.Detail == "" {
		t.Fatalf("bad key row = %#v, want unresolved with a reason", got)
	}
	if got := actions["no-endpoint"]; got.Action != "unresolved" || got.Detail == "" {
		t.Fatalf("no-endpoint row = %#v, want unresolved with a reason", got)
	}

	// The mapped pin is now served from the registry, and the legacy file is
	// preserved as migration evidence.
	pins, err := LoadPins(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pin, ok := pins["wiseman"]; !ok || pin.Session != "s1" || pin.PaneID != "w1:pJ" {
		t.Fatalf("post-import pins = %#v, want wiseman served from seats.json", pins)
	}
	if _, err := os.Stat(filepath.Join(dir, PinsFile)); err != nil {
		t.Fatalf("pins.json must be preserved: %v", err)
	}

	afterFirst, err := os.ReadFile(filepath.Join(dir, seats.File))
	if err != nil {
		t.Fatal(err)
	}

	// Second run: nothing new maps, nothing is invented, bytes unchanged.
	rows2, err := ImportPins(dir, time.Unix(1700000300, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows2 {
		if row.Action == "mapped" {
			t.Fatalf("second run remapped %#v; import must be idempotent", row)
		}
		if row.PinKey == "wiseman" && row.Action != "already-present" {
			t.Fatalf("wiseman on rerun = %#v, want already-present", row)
		}
	}
	afterSecond, err := os.ReadFile(filepath.Join(dir, seats.File))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterFirst) != string(afterSecond) {
		t.Fatalf("second run rewrote seats.json:\nfirst:\n%s\nsecond:\n%s", afterFirst, afterSecond)
	}
}
