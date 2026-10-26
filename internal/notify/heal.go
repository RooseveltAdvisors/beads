package notify

import (
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/seats"
)

// Heal repairs endpoints at the moment routing runs. It does not use a timer.
// A still-live agent session id rewrites a moved pane. The calling process
// rebinds its own seat when its name resolves to exactly one id, which covers
// a restart that minted a new pane and a new session file. An unknown or
// ambiguous name is left alone. Heal never creates a seat.
func (o *Outbox) Heal(selfName string, self Instance, live []Instance, now time.Time, haveLive bool) error {
	if haveLive {
		if _, err := o.Reconcile(live, now, 5*time.Minute); err != nil {
			return err
		}
	}
	return o.Rebind(selfName, self, now, live, haveLive)
}

// Rebind moves one already-registered seat to the caller's exact endpoint.
// The seat id and its aliases do not change. A seat that is live-bound to a
// different endpoint is a hold: exact pins win over same-name claims, so two
// agents sharing a name cannot flip a live binding. Rebind fills a void - an
// empty endpoint or one no live instance matches - which is the restart case
// the heal exists for. When liveness cannot be observed at all, a bound seat
// is held: a hold loses nothing (rows stay queued), a stolen pin loses the
// delivery.
func (o *Outbox) Rebind(name string, self Instance, now time.Time, live []Instance, haveLive bool) error {
	name = strings.TrimSpace(name)
	self.Session = strings.TrimSpace(self.Session)
	self.PaneID = strings.TrimSpace(self.PaneID)
	self.AgentSessionID = strings.TrimSpace(self.AgentSessionID)
	if name == "" || self.Session == "" || self.PaneID == "" {
		return nil
	}
	return seats.Update(o.dir, func(r seats.Registry) error {
		if !seats.Strict(r) {
			return nil
		}
		id, err := r.Resolve(name)
		if err != nil || id == "" {
			return nil
		}
		ep := seats.Endpoint{Session: self.Session, PaneID: self.PaneID, AgentSessionID: self.AgentSessionID}
		cur := r[id]
		if cur.Endpoint == ep && cur.Liveness == seats.Live {
			return nil
		}
		if holdsLiveBinding(cur, self, live, haveLive) {
			return nil
		}
		if endpointHeldByOtherSeat(r, id, self.PaneID) {
			// One endpoint, one seat. The caller's pane already belongs
			// to a registered seat: a name claim is not proof of a
			// second identity on the same pane.
			return nil
		}
		if callerIsFinished(self, live, haveLive) {
			// A finished agent's shell still runs commands. Its last
			// commands must never leave a seat pointing at a pane no
			// live agent will read: the rows would deliver and ack into
			// a dead pane forever.
			return nil
		}
		return r.Register(id, nil, ep, now)
	})
}

// holdsLiveBinding reports whether the seat must keep its registered
// endpoint against this caller. Pane occupancy is the durable, observable
// fact: while a live agent sits at the registered pane, the binding holds
// against every same-name claimant. The void case - no live agent at the
// registered pane - is the restart the heal exists for. With no liveness
// evidence at all the last observation decides: a held row retries, a
// stolen pin loses the delivery.
func holdsLiveBinding(cur seats.Seat, self Instance, live []Instance, haveLive bool) bool {
	if cur.Endpoint.PaneID == "" || cur.Endpoint.PaneID == self.PaneID {
		return false
	}
	if !haveLive {
		return cur.Liveness == seats.Live
	}
	for _, inst := range live {
		if inst.PaneID != cur.Endpoint.PaneID {
			continue
		}
		// The registered pane is occupied. Without session evidence on
		// both sides, stability wins. With it, the binding moves only
		// to the seat's own registered session, never to a stranger.
		if cur.Endpoint.AgentSessionID == "" || inst.AgentSessionID == "" {
			return true
		}
		return inst.AgentSessionID == cur.Endpoint.AgentSessionID ||
			self.AgentSessionID != cur.Endpoint.AgentSessionID
	}
	return false
}

// callerIsFinished reports whether the calling instance is a finished agent
// (done, dead, exited, or stopped). Discovery liveness is the evidence; with
// no evidence the void-fill rules still apply.
func callerIsFinished(self Instance, live []Instance, haveLive bool) bool {
	if !haveLive {
		return false
	}
	for _, inst := range live {
		if inst.PaneID != self.PaneID {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(inst.Status))
		return status == "done" || status == "dead" || status == "exited" || status == "stopped"
	}
	return false
}

// endpointHeldByOtherSeat reports whether paneID is the registered endpoint
// of a seat other than id.
func endpointHeldByOtherSeat(r seats.Registry, id, paneID string) bool {
	for otherID, seat := range r {
		if otherID != id && seat.Endpoint.PaneID == paneID {
			return true
		}
	}
	return false
}
