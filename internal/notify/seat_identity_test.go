package notify

import (
	"encoding/json"
	"os"

	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/seats"
)

func identityRegistry(t *testing.T) seats.Registry {
	t.Helper()
	r := seats.Registry{}
	if err := r.Register("seat:worker", []string{"worker", "old-worker"}, seats.Endpoint{Session: "agents", PaneID: "p1", AgentSessionID: "session-1"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSeatBackfillReportsUnresolvedAndIsIdempotent(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var legacy []byte
	for i, name := range []string{"old-worker", "arcs-fm", "worker"} {
		line, err := json.Marshal(Record{Seq: int64(i + 1), Seat: name, IssueID: "bd-1", Kind: KindDue})
		if err != nil {
			t.Fatal(err)
		}
		legacy = append(legacy, append(line, '\n')...)
	}
	if err := os.WriteFile(filepath.Join(o.Dir(), OutboxFile), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	r := identityRegistry(t)
	rows, err := o.Backfill(r, false, false)
	if err != nil || len(rows) != 3 || rows[1].Name != "arcs-fm" || rows[1].Action != "unresolved-held" {
		t.Fatalf("report lost dead string: %+v %v", rows, err)
	}
	if _, err := o.Backfill(r, true, false); err == nil {
		t.Fatal("silently flushed unknown seat")
	}
	rows, err = o.Backfill(r, true, true)
	if err != nil || rows[1].Action != "flushed" {
		t.Fatalf("flush report: %+v %v", rows, err)
	}
	pending, err := o.Pending("seat:worker")
	if err != nil || len(pending) != 2 {
		t.Fatalf("rekey: %+v %v", pending, err)
	}
	before, err := os.ReadFile(filepath.Join(o.Dir(), OutboxFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Backfill(r, true, true); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(o.Dir(), OutboxFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("repeated backfill rewrote canonical records")
	}
}

func TestSeatDeadEndpointStaysQueued(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := identityRegistry(t)
	s := r["seat:worker"]
	s.Liveness = seats.Dead
	r["seat:worker"] = s
	if err := seats.Save(o.Dir(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Enqueue("seat:worker", "bd-1", KindDue, "title"); err != nil {
		t.Fatal(err)
	}
	pins, err := o.LoadPins()
	if err != nil {
		t.Fatal(err)
	}
	decision := DecideDelivery("seat:worker", pins, []Instance{{Session: "agents", PaneID: "p1", AgentSessionID: "session-1", Harness: "pi", Status: "idle"}})
	if decision.OK || !decision.Escalate || decision.Reason != HoldDeadPin {
		t.Fatalf("dead seat routed: %+v", decision)
	}
	pending, err := o.Pending("seat:worker")
	if err != nil || len(pending) != 1 {
		t.Fatalf("lost held row: %v %v", pending, err)
	}
}

func TestSeatReconcilerMarksStaleAndRebindsExactSession(t *testing.T) {
	r := identityRegistry(t)
	now := time.Now().UTC()
	if dead := ReconcileSeats(r, nil, now, time.Minute); len(dead) != 0 || r["seat:worker"].Liveness != seats.Stale {
		t.Fatalf("not stale: %v", r)
	}
	if dead := ReconcileSeats(r, nil, now.Add(time.Minute), time.Minute); len(dead) != 1 || r["seat:worker"].Liveness != seats.Dead {
		t.Fatalf("not dead: %v", r)
	}
	ReconcileSeats(r, []Instance{{Session: "moved", PaneID: "p9", AgentSessionID: "session-1"}}, now.Add(2*time.Minute), time.Minute)
	if s := r["seat:worker"]; s.Liveness != seats.Live || s.Endpoint.PaneID != "p9" || s.MissingSince != nil {
		t.Fatalf("exact session not rebound: %+v", s)
	}
}
