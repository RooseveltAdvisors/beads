package issueops

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/seats"
)

// BackfillSeatIDsInTx is deliberately separate from schema migration: only an
// operator-supplied exact registry can decide what a historic name meant.
// Every string, including unresolved ones, is reported on every pass.
func BackfillSeatIDsInTx(ctx context.Context, tx DBTX, r seats.Registry, apply bool) ([]seats.BackfillRow, error) {
	var report []seats.BackfillRow
	for _, table := range []string{"issues", "wisps"} {
		//nolint:gosec // table comes from the two literals above
		rows, err := tx.QueryContext(ctx, "SELECT id, COALESCE(assignee,''), assignee_id, COALESCE(created_by,''), COALESCE(owner,''), COALESCE(actor,'') FROM "+table+" ORDER BY id")
		if err != nil {
			return report, err
		}
		type record struct{ id, name, assignedID, creator, owner, actor string }
		var records []record
		for rows.Next() {
			var row record
			if err := rows.Scan(&row.id, &row.name, &row.assignedID, &row.creator, &row.owner, &row.actor); err != nil {
				_ = rows.Close()
				return report, err
			}
			records = append(records, row)
		}
		err = rows.Err()
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return report, err
		}
		for _, rec := range records {
			for _, field := range []struct{ key, name string }{{"assignee", rec.name}, {"created_by", rec.creator}, {"owner", rec.owner}, {"actor", rec.actor}} {
				name := field.name
				if field.key == "assignee" && rec.assignedID != "" {
					name = rec.assignedID
				}
				if name == "" {
					continue
				}
				item := seats.BackfillRow{Table: table, IssueID: rec.id, Field: field.key, Name: field.name, Action: "unresolved"}
				id, err := r.Resolve(name)
				if err != nil {
					report = append(report, item)
					continue
				}
				item.SeatID, item.Action = id, "unchanged"
				needsWrite := field.name != id || (field.key == "assignee" && rec.assignedID != id)
				if needsWrite {
					item.Action = "resolved"
				}
				report = append(report, item)
				if !apply || !needsWrite {
					continue
				}
				query := fmt.Sprintf("UPDATE %s SET `%s` = ?, row_lock = ? WHERE id = ?", table, field.key)
				args := []any{id, FreshRowLock(), rec.id}
				if field.key == "assignee" {
					query = fmt.Sprintf("UPDATE %s SET assignee = ?, assignee_id = ?, row_lock = ? WHERE id = ?", table)
					args = []any{id, id, FreshRowLock(), rec.id}
				}
				if _, err := tx.ExecContext(ctx, query, args...); err != nil {
					return report, err
				}
			}
		}
	}
	history, err := backfillSeatHistory(ctx, tx, r, apply)
	report = append(report, history...)
	return report, err
}
