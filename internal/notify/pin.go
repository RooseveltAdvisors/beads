package notify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// PinsFile is the exact seat → herdr target map. Delivery never
	// discovers a pane; it only uses this file.
	PinsFile = "pins.json"
	// HoldsFile records seats whose drain held for parent escalation.
	HoldsFile = "holds.json"

	// HoldNoPin means the seat has no pin; the row stays queued.
	HoldNoPin = "no-pin"
	// HoldDeadPin means the pin exists but that exact target is not live.
	HoldDeadPin = "pinned-target-dead"
)

// Pin is an exact delivery target. Session+pane_id and/or agent_session_id
// must match a live herdr instance with no scoring or lookalike fallback.
type Pin struct {
	Session        string `json:"session,omitempty"`
	PaneID         string `json:"pane_id,omitempty"`
	AgentSessionID string `json:"agent_session_id,omitempty"`
	Source         string `json:"source,omitempty"`
}

// Valid reports whether the pin names at least one exact locator.
func (p Pin) Valid() bool {
	hasPane := strings.TrimSpace(p.Session) != "" && strings.TrimSpace(p.PaneID) != ""
	hasAgent := strings.TrimSpace(p.AgentSessionID) != ""
	return hasPane || hasAgent
}

// Matches is exact: every field the pin sets must equal the instance.
func (p Pin) Matches(inst Instance) bool {
	if !p.Valid() {
		return false
	}
	if s := strings.TrimSpace(p.Session); s != "" {
		if !strings.EqualFold(s, strings.TrimSpace(inst.Session)) {
			return false
		}
	}
	if id := strings.TrimSpace(p.PaneID); id != "" {
		if id != strings.TrimSpace(inst.PaneID) {
			return false
		}
	}
	if id := strings.TrimSpace(p.AgentSessionID); id != "" {
		if id != strings.TrimSpace(inst.AgentSessionID) {
			return false
		}
	}
	return true
}

// Delivery is the drain decision for one seat.
type Delivery struct {
	Instance Instance
	Pin      Pin
	OK       bool
	Escalate bool
	Reason   string
}

// DecideDelivery is the only delivery resolver. No pin or a dead pin holds
// the outbox row and marks escalation. Lookalike panes never win.
func DecideDelivery(seat string, pins map[string]Pin, live []Instance) Delivery {
	seat = strings.TrimSpace(strings.ToLower(seat))
	if seat == "" || seat == UnassignedSeat {
		return Delivery{Reason: HoldNoPin}
	}
	if pins == nil {
		return Delivery{Reason: HoldNoPin, Escalate: true}
	}
	pin, ok := pins[seat]
	if !ok || !pin.Valid() {
		return Delivery{Pin: pin, Reason: HoldNoPin, Escalate: true}
	}
	inst, found := FindPinnedInstance(pin, live)
	if !found {
		return Delivery{Pin: pin, Reason: HoldDeadPin, Escalate: true}
	}
	inst.MatchReason = "pin"
	inst.Score = 0
	return Delivery{Instance: inst, Pin: pin, OK: true}
}

// FindPinnedInstance returns the live instance that exactly matches pin.
func FindPinnedInstance(pin Pin, live []Instance) (Instance, bool) {
	for _, inst := range live {
		if pin.Matches(inst) {
			return inst, true
		}
	}
	return Instance{}, false
}

// LoadPins reads dir/pins.json. A missing file is an empty map, not an error.
func LoadPins(dir string) (map[string]Pin, error) {
	path := filepath.Join(dir, PinsFile)
	data, err := os.ReadFile(path) //nolint:gosec // G304: dir is the outbox path; PinsFile is a constant
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Pin{}, nil
		}
		return nil, fmt.Errorf("notify: read pins: %w", err)
	}
	raw := map[string]Pin{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("notify: parse pins: %w", err)
	}
	out := make(map[string]Pin, len(raw))
	for k, v := range raw {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" || key == UnassignedSeat {
			continue
		}
		out[key] = v
	}
	return out, nil
}

// LoadPins reads this outbox's pins.json.
func (o *Outbox) LoadPins() (map[string]Pin, error) {
	return LoadPins(o.dir)
}

// Hold is a durable parent-escalation mark for a seat that could not be
// delivered. Pending rows stay in the outbox.
type Hold struct {
	Reason         string `json:"reason"`
	Session        string `json:"session,omitempty"`
	PaneID         string `json:"pane_id,omitempty"`
	AgentSessionID string `json:"agent_session_id,omitempty"`
	Pending        int    `json:"pending,omitempty"`
	UpdatedAt      string `json:"updated_at"`
}

// SetHold records that drain held this seat (parent should escalate).
func (o *Outbox) SetHold(seat string, h Hold) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	seat = strings.ToLower(strings.TrimSpace(seat))
	if seat == "" {
		return fmt.Errorf("notify: hold requires a seat")
	}
	holds, err := o.loadHoldsLocked()
	if err != nil {
		return err
	}
	if h.UpdatedAt == "" {
		h.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	holds[seat] = h
	return o.saveHoldsLocked(holds)
}

// Holds returns the current escalation marks.
func (o *Outbox) Holds() (map[string]Hold, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.loadHoldsLocked()
}

// ClearHold drops a seat's escalation mark after a successful delivery.
func (o *Outbox) ClearHold(seat string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	seat = strings.ToLower(strings.TrimSpace(seat))
	if seat == "" {
		return fmt.Errorf("notify: clear hold requires a seat")
	}
	holds, err := o.loadHoldsLocked()
	if err != nil {
		return err
	}
	if _, ok := holds[seat]; !ok {
		return nil
	}
	delete(holds, seat)
	return o.saveHoldsLocked(holds)
}

func (o *Outbox) loadHoldsLocked() (map[string]Hold, error) {
	path := filepath.Join(o.dir, HoldsFile)
	data, err := os.ReadFile(path) //nolint:gosec // G304: o.dir is the outbox path; HoldsFile is a constant
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Hold{}, nil
		}
		return nil, err
	}
	out := map[string]Hold{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("notify: parse holds: %w", err)
	}
	norm := make(map[string]Hold, len(out))
	for k, v := range out {
		norm[strings.ToLower(k)] = v
	}
	return norm, nil
}

func (o *Outbox) saveHoldsLocked(holds map[string]Hold) error {
	path := filepath.Join(o.dir, HoldsFile)
	data, err := json.MarshalIndent(holds, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
