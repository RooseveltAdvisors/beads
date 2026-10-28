package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/beads/internal/seats"
)

// TestE2E_MintRestartReregisterDeliverForeignShellRefused is the durable
// agent-uuid journey:
//  1. mint: one immutable uuid plus rebind token, persisted in the registry
//     and the agent's durable state, derived from no pane/tab/session
//  2. bind: after a restart into a NEW pane the agent re-registers under the
//     same uuid and the endpoint re-asserts
//  3. route: the queue row carries the uuid; delivery resolves the uuid to
//     the live endpoint at send time and holds while it does not resolve
//  4. refuse: a foreign shell cannot register under that uuid
func TestE2E_MintRestartReregisterDeliverForeignShellRefused(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	statePath := filepath.Join(t.TempDir(), "agent-state.json")

	// 1. Mint (first registration), exactly what `bd seat register --state`
	//    does with no existing state.
	oldEndpoint := seats.Endpoint{Session: "wiseman", PaneID: "w1:pOLD", AgentSessionID: "session-old"}
	var id, token string
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		var err error
		id, token, err = r.Mint([]string{"dispatch-worker"}, oldEndpoint, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("minted id %q is not a uuid: %v", id, err)
	}
	if err := seats.WriteState(statePath, seats.AgentState{AgentUUID: id, Alias: "dispatch-worker", RebindToken: token}); err != nil {
		t.Fatal(err)
	}

	// Acceptance (1): the uuid is persisted in the registry and the durable
	// state; the registry holds only the token hash, never the bearer token.
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	seat, ok := r[id]
	if !ok {
		t.Fatalf("registry lost the minted uuid %s", id)
	}
	if len(seat.Aliases) != 1 || seat.Aliases[0] != "dispatch-worker" || seat.TokenSHA256 == "" {
		t.Fatalf("minted seat = %+v", seat)
	}
	raw, err := os.ReadFile(filepath.Join(o.Dir(), seats.File))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("registry persisted the bearer rebind token")
	}
	st, err := seats.ReadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.AgentUUID != id || st.RebindToken != token {
		t.Fatalf("durable state = %+v, want uuid %s and its token", st, id)
	}

	// The queue row carries the uuid even when the sender addresses the alias.
	rec, err := o.Enqueue("dispatch-worker", "bd-1", KindDue, "wake")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Seat != id {
		t.Fatalf("queue row seat = %q, want the uuid %s", rec.Seat, id)
	}

	// Acceptance (3): delivery resolves at send time and holds while the uuid
	// does not resolve to a live endpoint.
	pins, err := o.LoadPins()
	if err != nil {
		t.Fatal(err)
	}
	dec := DecideDelivery(id, pins, nil)
	if dec.OK || !dec.Escalate || dec.Reason != HoldDeadPin {
		t.Fatalf("unresolvable uuid delivered instead of holding: %+v", dec)
	}
	if pending, _ := o.Pending(id); len(pending) != 1 {
		t.Fatalf("held row lost: %v", pending)
	}

	// 2. Restart into a NEW pane: the agent re-registers under the uuid from
	//    its durable state; the endpoint re-asserts, the uuid never changes.
	newEndpoint := seats.Endpoint{Session: "wiseman", PaneID: "w1:pNEW", AgentSessionID: "session-new"}
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.RebindWithToken(st.AgentUUID, newEndpoint, st.RebindToken, now.Add(time.Minute))
	}); err != nil {
		t.Fatal(err)
	}
	r, err = seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 || r[id].Endpoint != newEndpoint {
		t.Fatalf("re-register did not re-assert the endpoint under the same uuid: %v", r)
	}

	// 3. Send-time resolution: pins loaded now name the NEW pane; delivery
	//    lands there, not on the pane the row was enqueued against.
	pins, err = o.LoadPins()
	if err != nil {
		t.Fatal(err)
	}
	live := []Instance{{Session: "wiseman", PaneID: "w1:pNEW", AgentSessionID: "session-new", Harness: "pi", Status: "idle"}}
	dec = DecideDelivery(id, pins, live)
	if !dec.OK || dec.Instance.PaneID != "w1:pNEW" {
		t.Fatalf("send-time resolution = %+v, want delivery to w1:pNEW", dec)
	}
	var argv [][]string
	discover := HerdrDiscoverer{runFn: func(args ...string) ([]byte, error) {
		argv = append(argv, append([]string{}, args...))
		return []byte("{}"), nil
	}}
	if err := discover.Deliver(dec.Instance, DefaultPrompt(rec), dec.Mode); err != nil {
		t.Fatal(err)
	}
	if len(argv) == 0 || !containsSeq(argv[len(argv)-1], "w1:pNEW") {
		t.Fatalf("delivery argv = %v, want the new pane addressed", argv)
	}
	if err := o.AckSeat(id, rec.Seq); err != nil {
		t.Fatal(err)
	}

	// 4. A foreign shell knows the uuid but holds no token: every register
	//    path refuses it and the endpoint stays with the legitimate owner.
	err = seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register(id, nil, seats.Endpoint{Session: "wiseman", PaneID: "w1:pEVIL"}, now.Add(2*time.Minute))
	})
	if err == nil {
		t.Fatal("foreign shell registered under the minted uuid")
	}
	err = seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.RebindWithToken(id, seats.Endpoint{Session: "wiseman", PaneID: "w1:pEVIL"}, "guessed-token", now.Add(2*time.Minute))
	})
	if err == nil {
		t.Fatal("foreign shell rebound the minted uuid with a wrong token")
	}
	r, err = seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r[id].Endpoint != newEndpoint {
		t.Fatalf("foreign shell moved the endpoint: %+v", r[id].Endpoint)
	}
}
