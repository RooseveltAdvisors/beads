package notify

import (
	"fmt"
	"sort"
	"time"

	"github.com/steveyegge/beads/internal/seats"
)

// ReconcileSeats observes herdr; it never creates, stops, or restarts agents.
// A moved endpoint can only be recovered by the registered agent session ID.
// A new agent session must explicitly re-register its immutable seat ID.
func ReconcileSeats(r seats.Registry, live []Instance, now time.Time, deadAfter time.Duration) []string {
	var dead []string
	for id, seat := range r {
		if seat.UpdatedAt.After(now) {
			continue
		}
		pin := Pin{Session: seat.Endpoint.Session, PaneID: seat.Endpoint.PaneID, AgentSessionID: seat.Endpoint.AgentSessionID}
		inst, found := FindPinnedInstance(pin, live)
		if !found && seat.Session != "" {
			for _, candidate := range live {
				if candidate.AgentSessionID == seat.Session {
					// An ambiguous exact session is a hold, never first-match routing.
					if found {
						found = false
						break
					}
					inst, found = candidate, true
				}
			}
		}
		if found {
			seat.Endpoint = seats.Endpoint{Session: inst.Session, PaneID: inst.PaneID, AgentSessionID: inst.AgentSessionID}
			seat.Liveness, seat.LastSeenAt, seat.MissingSince = seats.Live, now.UTC(), nil
		} else {
			if seat.MissingSince == nil {
				t := now.UTC()
				seat.MissingSince = &t
			}
			seat.Liveness = seats.Stale
			if now.Sub(*seat.MissingSince) >= deadAfter {
				seat.Liveness = seats.Dead
				dead = append(dead, id)
			}
		}
		seat.UpdatedAt = now.UTC()
		r[id] = seat
	}
	sort.Strings(dead)
	return dead
}

func (o *Outbox) Registry() (seats.Registry, error) { return seats.Load(o.dir) }

// ResolveSeat canonicalizes explicit CLI names, never queue routing keys.
// In a workspace with no registered seats (legacy free-text mode) the name is
// kept verbatim; strictness activates with the first registered seat.
func (o *Outbox) ResolveSeat(name string) (string, error) {
	r, err := o.Registry()
	if err != nil {
		return "", err
	}
	if !seats.Strict(r) {
		return name, nil
	}
	return r.Resolve(name)
}

// Reconcile records dead seats on the existing escalation rail even when they
// have no pending outbox rows. Discovery failures leave registry state intact.
func (o *Outbox) Reconcile(live []Instance, now time.Time, deadAfter time.Duration) (seats.Registry, error) {
	var r seats.Registry
	var dead []string
	if err := seats.Update(o.dir, func(current seats.Registry) error {
		r = current
		dead = ReconcileSeats(r, live, now, deadAfter)
		return nil
	}); err != nil {
		return nil, err
	}
	for _, id := range dead {
		pending, err := o.Pending(id)
		if err != nil {
			return nil, err
		}
		s := r[id]
		if err := o.SetHold(id, Hold{Reason: HoldDeadPin, Session: s.Endpoint.Session, PaneID: s.Endpoint.PaneID, Pending: len(pending)}); err != nil {
			return nil, fmt.Errorf("escalate dead seat %s: %w", id, err)
		}
	}
	return r, nil
}
