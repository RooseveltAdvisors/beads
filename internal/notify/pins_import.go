package notify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/steveyegge/beads/internal/seats"
)

// PinsImportRow is one legacy pin's migration outcome. Every pin is reported
// on every pass: mapped, already present, or unresolved with the reason, so
// the import can never silently bind some pins and drop others.
type PinsImportRow struct {
	PinKey string `json:"pin_key"`
	SeatID string `json:"seat_id,omitempty"`
	Action string `json:"action"` // mapped | already-present | unresolved
	Detail string `json:"detail,omitempty"`
}

// ImportPins migrates the legacy pins.json into seats.json, one-time and
// idempotent. The pin key becomes the immutable seat id (exact, never
// derived or sanitized) with the old name kept as its alias, and the pin's
// endpoint is preserved. A key that is not a valid seat id, or a pin without
// an exact endpoint, is reported unresolved and never invented into a seat.
// pins.json itself is left in place as migration evidence. Re-running maps
// nothing new and does not rewrite seats.json.
func ImportPins(dir string, now time.Time) ([]PinsImportRow, error) {
	data, err := os.ReadFile(filepath.Join(dir, PinsFile)) //nolint:gosec // fixed legacy file in the notify dir
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pins: %w", err)
	}
	raw := map[string]legacyPin{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse pins: %w", err)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	release, lockErr := seats.RegistryLock(dir)
	if lockErr != nil {
		return nil, lockErr
	}
	defer release()
	r, err := seats.Load(dir)
	if err != nil {
		return nil, err
	}
	var report []PinsImportRow
	changed := false
	for _, key := range keys {
		pin := raw[key]
		row := PinsImportRow{PinKey: key}
		if id, resolveErr := r.Resolve(key); resolveErr == nil && id != "" {
			row.SeatID, row.Action = id, "already-present"
			report = append(report, row)
			continue
		}
		if !pin.hasEndpoint() {
			row.Action, row.Detail = "unresolved", "pin has no exact endpoint"
			report = append(report, row)
			continue
		}
		if err := seats.ValidateID(key); err != nil {
			row.Action, row.Detail = "unresolved", err.Error()
			report = append(report, row)
			continue
		}
		seat := seats.Seat{
			Aliases:    []string{key},
			Session:    pin.AgentSessionID,
			Endpoint:   seats.Endpoint{Session: pin.Session, PaneID: pin.PaneID, AgentSessionID: pin.AgentSessionID},
			Liveness:   seats.Live,
			UpdatedAt:  now.UTC(),
			LastSeenAt: now.UTC(),
		}
		previous, had := r[key]
		r[key] = seat
		if err := r.Validate(); err != nil {
			if had {
				r[key] = previous
			} else {
				delete(r, key)
			}
			row.Action, row.Detail = "unresolved", err.Error()
			report = append(report, row)
			continue
		}
		row.SeatID, row.Action = key, "mapped"
		changed = true
		report = append(report, row)
	}
	if !changed {
		return report, nil
	}
	if err := seats.Save(dir, r); err != nil {
		return report, err
	}
	return report, nil
}

// hasEndpoint reports whether the legacy pin names an exact delivery target.
func (p legacyPin) hasEndpoint() bool {
	return (p.Session != "" && p.PaneID != "") || p.AgentSessionID != ""
}
