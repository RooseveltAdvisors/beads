package notify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/seats"
)

type RekeyRow struct {
	Seq     int64  `json:"seq"`
	IssueID string `json:"issue_id"`
	Name    string `json:"name"`
	SeatID  string `json:"seat_id,omitempty"`
	Action  string `json:"action"`
}

// Backfill reports EVERY pending row. Unknown names require an explicit flush
// option; they are never promoted into registry entries. Re-running is safe.
func (o *Outbox) Backfill(r seats.Registry, apply, flush bool) ([]RekeyRow, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	release, lockErr := seats.LockFile(filepath.Join(o.dir, "outbox.lock"))
	if lockErr != nil {
		return nil, lockErr
	}
	defer release()
	all, err := o.readAllLocked()
	if err != nil {
		return nil, err
	}
	acks, err := o.loadAcksLocked()
	if err != nil {
		return nil, err
	}
	var report []RekeyRow
	var acked, pending []Record
	changed, unresolved := false, false
	var highest int64
	for _, rec := range all {
		if rec.Seq > highest {
			highest = rec.Seq
		}
		if rec.Seq <= acks[strings.ToLower(rec.Seat)] {
			acked = append(acked, rec)
			continue
		}
		row := RekeyRow{Seq: rec.Seq, IssueID: rec.IssueID, Name: rec.Seat, Action: "unchanged"}
		id, resolveErr := r.Resolve(rec.Seat)
		if resolveErr != nil || id == "" {
			row.Action, unresolved, changed = "unresolved-held", true, true
			if flush {
				row.Action = "flushed"
			}
		} else {
			row.SeatID = id
			if rec.Seat != id {
				row.Action, changed = "rekeyed", true
			}
			rec.Seat = id
			pending = append(pending, rec)
		}
		report = append(report, row)
	}
	if !apply || !changed {
		return report, nil
	}
	if unresolved && !flush {
		return report, fmt.Errorf("unresolved notify names remain: review the report and rerun --apply --flush-unresolved to explicitly flush them")
	}
	// Preserve original evidence before replacing the ledger. Never overwrite a
	// previous migration backup, including when a crash left an incomplete pass.
	path := filepath.Join(o.dir, OutboxFile)
	backup := path + ".pre-seat-identity"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		data, err := os.ReadFile(path) //nolint:gosec // fixed ledger in workspace
		if err != nil {
			return report, err
		}
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			return report, err
		}
	} else if err != nil {
		return report, err
	}
	// Re-sequence above all existing acknowledgements before merging aliases.
	// Taking max(alias ack) would silently lose another alias's pending rows.
	for _, seq := range acks {
		if seq > highest {
			highest = seq
		}
	}
	for i := range pending {
		highest++
		pending[i].Seq = highest
	}
	var data []byte
	for _, rec := range append(acked, pending...) {
		line, err := json.Marshal(rec)
		if err != nil {
			return report, err
		}
		data = append(data, append(line, '\n')...)
	}
	// Counter first: a crash can leave a harmless gap, never reuse a sequence.
	if err := os.WriteFile(filepath.Join(o.dir, SeqFile), []byte(fmt.Sprintf("%d\n", highest)), 0o600); err != nil {
		return report, err
	}
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return report, err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return report, err
	}
	return report, nil
}
