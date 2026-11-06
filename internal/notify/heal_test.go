package notify

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/seats"
)

// mintTestSeat mints a uuid seat into the outbox registry and returns the
// durable state an agent would persist, plus the minted id.
func mintTestSeat(t *testing.T, o *Outbox, alias string, ep seats.Endpoint, now time.Time) (string, seats.AgentState) {
	t.Helper()
	var id, token string
	if err := seats.Update(o.Dir(), func(r seats.Registry) error {
		var err error
		id, token, err = r.Mint([]string{alias}, ep, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id, seats.AgentState{AgentUUID: id, Alias: alias, RebindToken: token}
}

// TestRebindKeepsUUIDAndMovesPaneAfterRestart is the restart case the model
// exists for: the agent comes up in a NEW pane, re-registers under its minted
// uuid with its token, and the endpoint re-asserts while the identity and
// aliases stay exactly as minted.
func TestRebindKeepsUUIDAndMovesPaneAfterRestart(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	id, state := mintTestSeat(t, o, "THE-FM", seats.Endpoint{
		Session: "firstmate", PaneID: "w1:pOLD", AgentSessionID: "session-old",
	}, now)

	err = o.Rebind(&state, Instance{
		Session: "firstmate", PaneID: "w1:pNEW", AgentSessionID: "session-new",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	got := r[id]
	// Endpoint carries agent-session only: pane is derived live, never stored.
	if got.Endpoint.AgentSessionID != "session-new" {
		t.Fatalf("endpoint = %#v, want agent-session new", got.Endpoint)
	}
	if got.Endpoint.PaneID != "" || got.Endpoint.Session != "" {
		t.Fatalf("endpoint must not store session/pane: %#v", got.Endpoint)
	}
	if len(got.Aliases) != 1 || got.Aliases[0] != "THE-FM" {
		t.Fatalf("aliases = %#v, want them unchanged", got.Aliases)
	}
	if got.Liveness != seats.Live {
		t.Fatalf("liveness = %q, want live after re-register", got.Liveness)
	}
}

// TestRebindWithoutDurableStateGuessesNothing pins the deleted lane-alias
// guessing layer: a caller that cannot present a durable state never moves a
// seat, even when its name and pane read exactly like a registered alias.
// Identity is the uuid plus token, not a resolved name.
func TestRebindWithoutDurableStateGuessesNothing(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	id, _ := mintTestSeat(t, o, "wiseman", seats.Endpoint{Session: "wiseman", PaneID: "w1:pP7"}, now)

	self := Instance{Session: "wiseman", PaneID: "w1:p3G", AgentSessionID: "session-stranger"}
	// No state at all.
	if err := o.Rebind(nil, self, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// A state-shaped value with no uuid and no token.
	if err := o.Rebind(&seats.AgentState{Alias: "wiseman"}, self, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r[id].Endpoint.PaneID != "w1:pP7" {
		t.Fatalf("stateless caller moved the seat to %s", r[id].Endpoint.PaneID)
	}
	if len(r) != 1 {
		t.Fatalf("stateless caller invented seats: %v", r)
	}
}

// TestForeignShellCannotRebindAMintedUUID pins the refusal half of the model:
// a shell that knows the uuid but never held the durable state cannot
// re-register it, on either the best-effort heal path (registry untouched) or
// the explicit registry path (error).
func TestForeignShellCannotRebindAMintedUUID(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	id, _ := mintTestSeat(t, o, "wiseman", seats.Endpoint{Session: "wiseman", PaneID: "w1:pP7"}, now)

	foreign := seats.AgentState{AgentUUID: id, Alias: "wiseman", RebindToken: "not-the-token"}
	self := Instance{Session: "wiseman", PaneID: "w1:pEVIL", AgentSessionID: "session-evil"}
	if err := o.Rebind(&foreign, self, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r[id].Endpoint.PaneID != "w1:pP7" {
		t.Fatalf("foreign shell moved the seat to %s", r[id].Endpoint.PaneID)
	}

	err = seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.RebindWithToken(id, seats.Endpoint{Session: "wiseman", PaneID: "w1:pEVIL"}, "not-the-token", now)
	})
	if err == nil {
		t.Fatal("wrong token rebind accepted")
	}
	err = seats.Update(o.Dir(), func(r seats.Registry) error {
		return r.Register(id, nil, seats.Endpoint{Session: "wiseman", PaneID: "w1:pEVIL"}, now)
	})
	if err == nil {
		t.Fatal("token-protected seat accepted the unauthenticated register path")
	}
	r, err = seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r[id].Endpoint.PaneID != "w1:pP7" {
		t.Fatalf("refused rebinds still moved the seat to %s", r[id].Endpoint.PaneID)
	}
}

// TestHealRewritesPaneWhenSessionFileSurvives keeps the reconcile recovery:
// herdr restarted the pane but the agent session file survived, so the
// observation moves the endpoint. No caller state is involved.
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
	if err := o.Heal(nil, Instance{}, live, now.Add(time.Second), true); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r["portal-ops"].Endpoint.AgentSessionID == "" || r["portal-ops"].Liveness != seats.Live {
		t.Fatalf("reconcile endpoint = %#v liveness=%s", r["portal-ops"].Endpoint, r["portal-ops"].Liveness)
	}
}

// TestHealRefusesRebindFromDeadProcessEnv is the regression for the
// 2026-11-06 clobber: a process carrying the wiseman durable state with a
// dead session file in its env re-asserted a dead endpoint on every bd run,
// overwriting a healthy rebind 8 minutes after it was written. A rebind whose
// self does not resolve to a live instance must be skipped.
func TestHealRefusesRebindFromDeadProcessEnv(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	id, state := mintTestSeat(t, o, "wiseman", seats.Endpoint{
		Session: "wiseman", PaneID: "w1:pHEALTHY", AgentSessionID: "live-session",
	}, now)

	// Live inventory has a different agent; the caller's env claims a dead session.
	live := []Instance{{
		Session: "wiseman", PaneID: "w9:p9", AgentSessionID: "other-session",
	}}
	self := Instance{
		Session: "wiseman", PaneID: "w9:p9",
		AgentSessionID: "dead-nov3-session",
	}
	if err := o.Heal(&state, self, live, now.Add(time.Minute), true); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	got := r[id]
	if got.Endpoint.AgentSessionID != "live-session" {
		t.Fatalf("dead-env rebind clobbered endpoint: %#v, want live-session preserved", got.Endpoint)
	}
	if got.Endpoint.PaneID != "w1:pHEALTHY" {
		t.Fatalf("endpoint pane changed: %#v, want w1:pHEALTHY", got.Endpoint)
	}
}

// The same self when it IS live rebinds normally.
func TestHealRebindsWhenSelfIsLive(t *testing.T) {
	o, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	id, state := mintTestSeat(t, o, "wiseman", seats.Endpoint{
		Session: "wiseman", PaneID: "w1:pOLD", AgentSessionID: "old-session",
	}, now)
	live := []Instance{{
		Session: "wiseman", PaneID: "w1:pNOW", AgentSessionID: "live-session",
	}}
	self := Instance{
		Session: "wiseman", PaneID: "w1:pNOW", AgentSessionID: "live-session",
	}
	if err := o.Heal(&state, self, live, now.Add(time.Minute), true); err != nil {
		t.Fatal(err)
	}
	r, err := seats.Load(o.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if r[id].Endpoint.AgentSessionID != "live-session" {
		t.Fatalf("live self did not rebind: %#v", r[id].Endpoint)
	}
}
