package notify

import (
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/seats"
)

// Heal reconciles liveness from herdr discovery, then re-asserts the caller's
// endpoint under its durable uuid. It does not use a timer. Identity comes
// only from the agent's durable state (uuid + rebind token): no name, tab
// label, pane, or session is ever resolved into an identity here, and a
// caller without durable state reconciles but never moves a seat.
func (o *Outbox) Heal(state *seats.AgentState, self Instance, live []Instance, now time.Time, haveLive bool) error {
	if haveLive {
		if _, err := o.Reconcile(live, now, 5*time.Minute); err != nil {
			return err
		}
	}
	// A rebind re-asserts the caller's endpoint from ITS process env
	// (HERDR_* / PI_SESSION_FILE). A process that outlives its herdr
	// entry - a detached pane, a lingering shell - carries a session
	// file no live instance reports, and its blind rebind clobbers a
	// healthy endpoint with a dead one (observed twice 2026-11-06: the
	// wiseman seat was overwritten back to a dead Nov 3 session minutes
	// after a correct rebind, including through the discovery-failure
	// path). Never write an endpoint we cannot verify: no live match -
	// whether discovery failed or self is simply dead - skips the rebind
	// and leaves rows to hold. Deliberate re-registration remains the
	// explicit `bd seat register --state` path.
	if !haveLive || !selfMatchesLive(self, live) {
		return nil
	}
	return o.Rebind(state, self, now)
}

// selfMatchesLive reports whether the caller's own instance is among the live
// inventory. A missing AgentSessionID falls back to session+pane, the legacy
// locator for callers that predate agent-session env.
func selfMatchesLive(self Instance, live []Instance) bool {
	if strings.TrimSpace(self.AgentSessionID) != "" {
		for _, inst := range live {
			if strings.TrimSpace(inst.AgentSessionID) == strings.TrimSpace(self.AgentSessionID) {
				return true
			}
		}
		return false
	}
	if strings.TrimSpace(self.Session) == "" || strings.TrimSpace(self.PaneID) == "" {
		return false
	}
	for _, inst := range live {
		if strings.TrimSpace(inst.Session) == strings.TrimSpace(self.Session) &&
			strings.TrimSpace(inst.PaneID) == strings.TrimSpace(self.PaneID) {
			return true
		}
	}
	return false
}

// Rebind re-asserts the endpoint of the seat the caller's durable state
// names. The uuid is the identity; the rebind token proves the caller is the
// agent the uuid was minted for, so a foreign shell cannot take the seat
// over. The endpoint is disposable: every start re-asserts it, and delivery
// resolves it at send time. There are no occupancy, exclusivity, or
// caller-status guards left: the token is the whole claim.
//
// Rebind is best-effort on the command path: a missing state, an unknown
// uuid, or a token refusal leaves the registry untouched and never fails the
// caller's command. The explicit `bd seat register` path surfaces those
// errors instead.
func (o *Outbox) Rebind(state *seats.AgentState, self Instance, now time.Time) error {
	self.Session = strings.TrimSpace(self.Session)
	self.PaneID = strings.TrimSpace(self.PaneID)
	self.AgentSessionID = strings.TrimSpace(self.AgentSessionID)
	if state == nil || strings.TrimSpace(state.AgentUUID) == "" || strings.TrimSpace(state.RebindToken) == "" {
		return nil
	}
	if self.Session == "" || self.PaneID == "" {
		return nil
	}
	return seats.Update(o.dir, func(r seats.Registry) error {
		ep := seats.Endpoint{AgentSessionID: self.AgentSessionID}
		if cur, ok := r[state.AgentUUID]; ok && cur.Endpoint == ep && cur.Liveness == seats.Live {
			return nil
		}
		// A refused rebind (unknown uuid, token mismatch) is a hold, not a
		// command failure: rows stay queued for the legitimate owner.
		_ = r.RebindWithToken(state.AgentUUID, ep, state.RebindToken, now)
		return nil
	})
}
