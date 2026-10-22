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
	}, now.Add(time.Minute))
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
	if err := o.Rebind("not-a-seat", Instance{Session: "wiseman", PaneID: "w1:p9"}, now); err != nil {
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
