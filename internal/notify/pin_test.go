package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func lookalikeInstances() []Instance {
	return []Instance{
		{Session: "firstmate", PaneID: "w1:p9", Harness: "pi", Status: "idle", Title: "π - firstmate", Focused: true, AgentSessionID: "stale-pi-2026-09-13"},
		{Session: "firstmate", PaneID: "w0:p2", Harness: "claude", Status: "idle", Title: "Arcs Macro ops", Cwd: "/work/firstmate"},
		{Session: "adhoc", PaneID: "w1:p3", Harness: "codex", Status: "idle", Title: "π - firstmate", Focused: true},
		{Session: "firstmate", PaneID: "w6:p2", Harness: "claude", Status: "working", Title: "advisory"},
	}
}

func TestPinnedSeatDeliversExactPaneDespiteLookalikes(t *testing.T) {
	t.Parallel()
	pins := map[string]Pin{
		"firstmate": {Session: "firstmate", PaneID: "w6:p2", Source: "lock"},
	}
	got := DecideDelivery("firstmate", pins, lookalikeInstances())
	if !got.OK || got.Escalate {
		t.Fatalf("want deliver, got %+v", got)
	}
	if got.Instance.PaneID != "w6:p2" || got.Instance.Session != "firstmate" {
		t.Fatalf("delivered lookalike instead of pin: %+v", got.Instance)
	}
	if got.Instance.MatchReason != "pin" || got.Instance.Score != 0 {
		t.Fatalf("delivery must not carry a fuzzy score: %+v", got.Instance)
	}
}

func TestUnpinnedSeatHoldsAndEscalates(t *testing.T) {
	t.Parallel()
	got := DecideDelivery("firstmate", map[string]Pin{}, lookalikeInstances())
	if got.OK {
		t.Fatalf("unpinned seat must not deliver, got %+v", got.Instance)
	}
	if !got.Escalate || got.Reason != HoldNoPin {
		t.Fatalf("want escalate no-pin, got %+v", got)
	}
	got = DecideDelivery("firstmate", nil, lookalikeInstances())
	if got.OK || !got.Escalate || got.Reason != HoldNoPin {
		t.Fatalf("nil pins: %+v", got)
	}
}

func TestDeadPinnedTargetHoldsAndEscalates(t *testing.T) {
	t.Parallel()
	pins := map[string]Pin{
		"firstmate": {Session: "firstmate", PaneID: "w99:p1"},
	}
	got := DecideDelivery("firstmate", pins, lookalikeInstances())
	if got.OK {
		t.Fatalf("dead pin must not deliver to a lookalike: %+v", got.Instance)
	}
	if !got.Escalate || got.Reason != HoldDeadPin {
		t.Fatalf("want escalate pinned-target-dead, got %+v", got)
	}
	if got.Pin.PaneID != "w99:p1" {
		t.Fatalf("hold should retain the dead pin, got %+v", got.Pin)
	}
}

func TestPinAgentSessionIDExact(t *testing.T) {
	t.Parallel()
	pins := map[string]Pin{
		"firstmate": {AgentSessionID: "stale-pi-2026-09-13"},
	}
	got := DecideDelivery("firstmate", pins, lookalikeInstances())
	if !got.OK || got.Instance.PaneID != "w1:p9" {
		t.Fatalf("agent_session_id pin: %+v", got)
	}
	pins["firstmate"] = Pin{Session: "firstmate", PaneID: "w1:p9", AgentSessionID: "other"}
	got = DecideDelivery("firstmate", pins, lookalikeInstances())
	if got.OK || got.Reason != HoldDeadPin {
		t.Fatalf("mismatched agent_session_id must not match pane-only: %+v", got)
	}
}

func TestUnassignedNeverDelivers(t *testing.T) {
	t.Parallel()
	pins := map[string]Pin{UnassignedSeat: {Session: "firstmate", PaneID: "w1:p9"}}
	got := DecideDelivery("unassigned", pins, lookalikeInstances())
	if got.OK || got.Escalate {
		t.Fatalf("unassigned: %+v", got)
	}
}

func TestLoadPinsAndHolds(t *testing.T) {
	beads := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	o, err := Open(beads)
	if err != nil {
		t.Fatal(err)
	}
	pins, err := o.LoadPins()
	if err != nil || len(pins) != 0 {
		t.Fatalf("missing pins.json: %v %+v", err, pins)
	}
	body := []byte(`{
  "FirstMate": {"session":"firstmate","pane_id":"w6:p2","source":"lock"},
  "wiseman": {"session":"wiseman","pane_id":"w1:pJ"}
}`)
	if err := os.WriteFile(filepath.Join(o.Dir(), PinsFile), body, 0o644); err != nil {
		t.Fatal(err)
	}
	pins, err = o.LoadPins()
	if err != nil {
		t.Fatal(err)
	}
	if pins["firstmate"].PaneID != "w6:p2" || pins["wiseman"].Session != "wiseman" {
		t.Fatalf("pins = %+v", pins)
	}

	if err := o.SetHold("firstmate", Hold{Reason: HoldDeadPin, Session: "firstmate", PaneID: "w99:p1", Pending: 2}); err != nil {
		t.Fatal(err)
	}
	holds, err := o.Holds()
	if err != nil {
		t.Fatal(err)
	}
	h, ok := holds["firstmate"]
	if !ok || h.Reason != HoldDeadPin || h.PaneID != "w99:p1" || h.Pending != 2 {
		t.Fatalf("hold = %+v ok=%v", h, ok)
	}
	if err := o.ClearHold("firstmate"); err != nil {
		t.Fatal(err)
	}
	if err := o.ClearHold("firstmate"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPinsRejectsCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PinsFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPins(dir); err == nil {
		t.Fatal("expected parse error")
	}
}
