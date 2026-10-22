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
		return r.Register(id, nil, ep, now)
	})
}

// holdsLiveBinding reports whether the seat is bound to an endpoint that is
// not the caller's and that still resolves to a live agent instance. With no
// liveness evidence the last observation decides; a live binding is held.
func holdsLiveBinding(cur seats.Seat, self Instance, live []Instance, haveLive bool) bool {
	if cur.Endpoint.PaneID == "" || cur.Endpoint.PaneID == self.PaneID {
		return false
	}
	if haveLive {
		pin := Pin{Session: cur.Endpoint.Session, PaneID: cur.Endpoint.PaneID, AgentSessionID: cur.Endpoint.AgentSessionID}
		_, ok := FindPinnedInstance(pin, live)
		return ok
	}
	return cur.Liveness == seats.Live
}
