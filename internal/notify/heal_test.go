package notify

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/seats"
)

func TestRebindKeepsIDAndMovesPaneAfterRestart(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register("firstmate", []string{"firstmate", "THE-FM"}, seats.Endpoint{
			Session: "firstmate", PaneID: "w1:pOLD", AgentSessionID: "session-old",
		}, now)
	}); err != nil {
		t.Fatal(err)
	}
	err = o.Rebind("THE-FM", Instance{
		Session: "firstmate", PaneID: "w1:pNEW", AgentSessionID: "session-new",
	}, now.Add(time.Minute), []Instance{{
		Session: "firstmate", PaneID: "w1:pNEW", AgentSessionID: "session-new",
	}}, true)
	if err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	got := r["firstmate"]
	if got.Endpoint.PaneID != "w1:pNEW" || got.Endpoint.AgentSessionID != "session-new" {
		t.Fatalf("endpoint = %#v, want new pane and session", got.Endpoint)
	}
	if len(got.Aliases) != 2 || got.Aliases[0] != "firstmate" || got.Aliases[1] != "THE-FM" {
		t.Fatalf("aliases = %#v, want them unchanged", got.Aliases)
	}
}

func TestRebindDoesNotInventOrStealASeat(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register("wiseman", []string{"wiseman"}, seats.Endpoint{Session: "wiseman", PaneID: "w1:p1"}, now)
	}); err != nil {
		t.Fatal(err)
	}
	if err := o.Rebind("not-a-seat", Instance{Session: "wiseman", PaneID: "w1:p9"}, now, nil, false); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r["not-a-seat"]; ok {
		t.Fatal("unknown name created a seat")
	}
	if r["wiseman"].Endpoint.PaneID != "w1:p1" {
		t.Fatalf("unrelated seat moved to %s", r["wiseman"].Endpoint.PaneID)
	}
}

func TestHealRewritesPaneWhenSessionFileSurvives(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register("portal-ops", []string{"portal-ops"}, seats.Endpoint{
			Session: "firstmate", PaneID: "w1:pOLD", AgentSessionID: "same-session",
		}, now)
	}); err != nil {
		t.Fatal(err)
	}
	live := []Instance{{
		Session: "firstmate", PaneID: "w1:pNEW", AgentSessionID: "same-session",
	}}
	if err := o.Heal("", Instance{}, live, now.Add(time.Second), true); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r["portal-ops"].Endpoint.PaneID != "w1:pNEW" || r["portal-ops"].Liveness != seats.Live {
		t.Fatalf("reconcile endpoint = %#v liveness=%s", r["portal-ops"].Endpoint, r["portal-ops"].Liveness)
	}
}

// TestSameNameCallerCannotStealALiveSeat pins the determinism contract: two
// agents sharing a seat name cannot flip a live binding. The registered
// endpoint wins while a live instance matches it; the same-name caller is a
// hold, and with no liveness evidence the last observation decides (live is
// held). This is the 2026-10-22 incident: a platform shell claiming the
// wiseman seat re-steered the pin away from the agent the captain named, and
// the seat's due notifies never woke it.
func TestSameNameCallerCannotStealALiveSeat(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register("wiseman", []string{"wiseman"}, seats.Endpoint{
			Session: "wiseman", PaneID: "w1:pP7", AgentSessionID: "session-owner",
		}, now)
	}); err != nil {
		t.Fatal(err)
	}
	other := Instance{Session: "wiseman", PaneID: "w1:p3G", AgentSessionID: "session-intruder"}
	owner := Instance{Session: "wiseman", PaneID: "w1:pP7", AgentSessionID: "session-owner"}
	live := []Instance{owner, other}

	// Observed liveness: the owner's endpoint is live and not the caller's.
	if err := o.Rebind("wiseman", other, now.Add(time.Minute), live, true); err != nil {
		t.Fatal(err)
	}
	// No liveness evidence at all: the last observation (live) decides.
	if err := o.Rebind("wiseman", other, now.Add(2*time.Minute), nil, false); err != nil {
		t.Fatal(err)
	}

	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	got := r["wiseman"]
	if got.Endpoint.PaneID != "w1:pP7" || got.Endpoint.AgentSessionID != "session-owner" {
		t.Fatalf("endpoint = %#v, want the live binding to hold", got.Endpoint)
	}

	// The restart case still moves: the owner's endpoint is gone from the
	// live set, so the void is the caller's to fill.
	if err := o.Rebind("wiseman", other, now.Add(3*time.Minute), []Instance{other}, true); err != nil {
		t.Fatal(err)
	}
	r, err = seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r["wiseman"].Endpoint.PaneID != "w1:p3G" {
		t.Fatalf("endpoint = %#v, want the void filled by the caller", r["wiseman"].Endpoint)
	}
}
