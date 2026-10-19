package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutboxEnqueuePendingAck(t *testing.T) {
	beads := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	o, err := openRegisteredTestOutbox(t, beads)
	if err != nil {
		t.Fatal(err)
	}

	r1, err := o.Enqueue("wiseman", "fm-a", KindDue, "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := o.Enqueue("wiseman", "fm-b", KindDue, "Beta")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Enqueue("firstmate", "fm-c", KindEscalate, "Charlie"); err != nil {
		t.Fatal(err)
	}
	if r1.Seq != 1 || r2.Seq != 2 {
		t.Fatalf("seq = %d,%d want 1,2", r1.Seq, r2.Seq)
	}

	pending, err := o.Pending("wiseman")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending wiseman = %d want 2", len(pending))
	}

	if err := o.AckSeat("wiseman", r1.Seq); err != nil {
		t.Fatal(err)
	}
	pending, err = o.Pending("wiseman")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].IssueID != "fm-b" {
		t.Fatalf("after partial ack: %+v", pending)
	}

	if err := o.AckSeat("wiseman", r2.Seq); err != nil {
		t.Fatal(err)
	}
	pending, err = o.Pending("wiseman")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("after full ack: %+v", pending)
	}

	all, err := o.Pending("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Seat != "firstmate" {
		t.Fatalf("other seat: %+v", all)
	}
}

func TestEmptySeatBecomesUnassigned(t *testing.T) {
	beads := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	o, err := openRegisteredTestOutbox(t, beads)
	if err != nil {
		t.Fatal(err)
	}
	r, err := o.Enqueue("", "fm-x", KindDue, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Seat != UnassignedSeat {
		t.Fatalf("seat = %q", r.Seat)
	}
}

func TestOutboxEnqueueUniqueSkipsUnackedDuplicate(t *testing.T) {
	beads := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	o, err := openRegisteredTestOutbox(t, beads)
	if err != nil {
		t.Fatal(err)
	}
	enqueue := func(seat, id string, kind Kind) bool {
		t.Helper()
		_, added, err := o.EnqueueUnique(seat, id, kind, "")
		if err != nil {
			t.Fatal(err)
		}
		return added
	}
	if !enqueue("alice", "fm-1", KindDue) {
		t.Fatal("first due row not added")
	}
	if enqueue("Alice", "fm-1", KindDue) {
		t.Fatal("duplicate unacked due row added")
	}
	if !enqueue("alice", "fm-1", KindEscalate) || !enqueue("bob", "fm-1", KindDue) || !enqueue("alice", "fm-2", KindDue) {
		t.Fatal("distinct seat/issue/kind rows must be added")
	}
	pending, err := o.Pending("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Fatalf("alice pending = %+v, want 3 rows", pending)
	}
	if err := o.AckSeat("alice", pending[len(pending)-1].Seq); err != nil {
		t.Fatal(err)
	}
	if !enqueue("alice", "fm-1", KindDue) {
		t.Fatal("due row must re-fire after ack")
	}
}

func TestOutboxPendingIsExactSeat(t *testing.T) {
	beads := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatal(err)
	}
	o, err := openRegisteredTestOutbox(t, beads)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []struct{ seat, id string }{
		{"alice", "fm-alice"},
		{"bob", "fm-bob"},
		{"", "fm-unassigned"},
	} {
		if _, err := o.Enqueue(rec.seat, rec.id, KindDue, ""); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := o.Pending("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].IssueID != "fm-alice" {
		t.Fatalf("alice pending = %+v, want alice only", pending)
	}
}
