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

// TestRebindCannotStealAnOccupiedPane pins the 2026-10-23 incident class: a
// same-name shell must not move a seat whose registered pane hosts a live
// agent. Pane occupancy is the durable fact - the discovery often leaves
// AgentSessionID empty, and a full-pin match then read every binding as
// stale, so any claimant won and notifications followed the thief.
func TestRebindCannotStealAnOccupiedPane(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register("wiseman", []string{"wiseman"}, seats.Endpoint{
			Session: "wiseman", PaneID: "w1:pP7", AgentSessionID: "session-owner",
		}, now)
	}); err != nil {
		t.Fatal(err)
	}
	// The registered pane is occupied. The discovery carries no session id
	// on the instance - the gap that made every binding read stale.
	occupant := Instance{Session: "wiseman", PaneID: "w1:pP7", AgentSessionID: ""}
	other := Instance{Session: "wiseman", PaneID: "w1:p3G", AgentSessionID: "session-intruder"}

	if err := o.Rebind("wiseman", other, now.Add(time.Minute), []Instance{occupant, other}, true); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r["wiseman"].Endpoint.PaneID != "w1:pP7" {
		t.Fatalf("endpoint = %#v, want the occupied pane to hold", r["wiseman"].Endpoint)
	}
}

// TestRebindRespectsOneEndpointOneSeat pins the second incident class: a
// caller whose pane already belongs to a registered seat may not claim a
// second seat onto the same pane.
func TestRebindRespectsOneEndpointOneSeat(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		if err := r.Register("beads", []string{"beads"}, seats.Endpoint{
			Session: "wiseman", PaneID: "w1:p3G", AgentSessionID: "session-beads",
		}, now); err != nil {
			return err
		}
		return r.Register("firstmate", []string{"firstmate"}, seats.Endpoint{
			Session: "firstmate", PaneID: "w1:pOLD", AgentSessionID: "session-fm",
		}, now)
	}); err != nil {
		t.Fatal(err)
	}
	// The firstmate binding's pane is gone (restart void), but the caller
	// lives on beads' pane. One endpoint, one seat.
	caller := Instance{Session: "wiseman", PaneID: "w1:p3G", AgentSessionID: "session-beads"}
	if err := o.Rebind("firstmate", caller, now.Add(time.Minute), []Instance{caller}, true); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r["firstmate"].Endpoint.PaneID != "w1:pOLD" {
		t.Fatalf("firstmate endpoint = %#v, want it held (pane belongs to seat beads)", r["firstmate"].Endpoint)
	}
}

// TestRebindMovesToTheSeatsOwnNewPane keeps the moved-owner case working: the
// seat's own session appears at a new pane while a stranger occupies the old
// one, and the seat follows its own session.
func TestRebindMovesToTheSeatsOwnNewPane(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register("wiseman", []string{"wiseman"}, seats.Endpoint{
			Session: "wiseman", PaneID: "w1:pOLD", AgentSessionID: "session-owner",
		}, now)
	}); err != nil {
		t.Fatal(err)
	}
	stranger := Instance{Session: "wiseman", PaneID: "w1:pOLD", AgentSessionID: "session-stranger"}
	moved := Instance{Session: "wiseman", PaneID: "w1:pNEW", AgentSessionID: "session-owner"}
	if err := o.Rebind("wiseman", moved, now.Add(time.Minute), []Instance{stranger, moved}, true); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r["wiseman"].Endpoint.PaneID != "w1:pNEW" {
		t.Fatalf("endpoint = %#v, want the seat to follow its own session", r["wiseman"].Endpoint)
	}
}

// TestFinishedCallerCannotRebindASeat pins the 2026-10-26 incident class: a
// finished agent's shell still runs commands, and its last commands moved the
// seat to a pane no live agent would ever read. The reminders then delivered
// and acked into a dead pane while the live owner got nothing.
func TestFinishedCallerCannotRebindASeat(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register("wiseman", []string{"wiseman"}, seats.Endpoint{
			Session: "wiseman", PaneID: "w1:pP7", AgentSessionID: "session-owner",
		}, now)
	}); err != nil {
		t.Fatal(err)
	}
	// The registered pane is NOT occupied here - the owner's agent is
	// gone from discovery - so the void-fill would otherwise accept the
	// claim. The caller is a finished agent whose shell still runs.
	finished := Instance{Session: "wiseman", PaneID: "w1:pWC", AgentSessionID: "session-finished", Status: "done"}
	if err := o.Rebind("wiseman", finished, now.Add(time.Minute), []Instance{finished}, true); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r["wiseman"].Endpoint.PaneID != "w1:pP7" {
		t.Fatalf("endpoint = %#v, want the seat to keep its live owner, not the finished caller", r["wiseman"].Endpoint)
	}
}
