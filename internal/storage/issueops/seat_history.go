package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/seats"
	"github.com/steveyegge/beads/internal/storage/rowid"
)

// Historic audit IDs are content derived. When the actor changes, re-key the
// row using the same derivation and duplicate ordinal discipline as inserts.
// No timestamps or issue activity are manufactured by this migration.
func backfillSeatHistory(ctx context.Context, tx DBTX, r seats.Registry, apply bool) ([]seats.BackfillRow, error) {
	var report []seats.BackfillRow
	for _, spec := range []struct {
		table, field, columns, key string
		actorIndex                 int
		derived                    bool
	}{
		{"events", "actor", "issue_id,event_type,actor,old_value,new_value,comment,CAST(created_at AS CHAR)", "id", 2, true},
		{"wisp_events", "actor", "issue_id,event_type,actor,old_value,new_value,comment,CAST(created_at AS CHAR)", "id", 2, true},
		{"comments", "author", "issue_id,author,text,CAST(created_at AS CHAR)", "id", 1, true},
		{"wisp_comments", "author", "issue_id,author,text,CAST(created_at AS CHAR)", "id", 1, true},
		{"dependencies", "created_by", "issue_id,created_by", "id", 1, false},
		{"wisp_dependencies", "created_by", "issue_id,created_by", "id", 1, false},
		{"bd_events_journal", "actor", "issue_id,actor", "seq", 1, false},
		{"leases", "holder", "issue_id,holder", "issue_id", 1, false},
	} {
		//nolint:gosec // descriptor names and columns are fixed literals above
		rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT %s,%s FROM %s ORDER BY %s", spec.key, spec.columns, spec.table, spec.key))
		if err != nil {
			return report, err
		}
		type row struct {
			key    string
			values []sql.NullString
		}
		var records []row
		taken := map[string]bool{}
		for rows.Next() {
			entry := row{values: make([]sql.NullString, strings.Count(spec.columns, ",")+1)}
			dests := []any{&entry.key}
			for i := range entry.values {
				dests = append(dests, &entry.values[i])
			}
			if err := rows.Scan(dests...); err != nil {
				_ = rows.Close()
				return report, err
			}
			records = append(records, entry)
			taken[entry.key] = true
		}
		err = rows.Err()
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return report, err
		}
		for _, entry := range records {
			name := entry.values[spec.actorIndex].String
			if name == "" {
				continue
			}
			item := seats.BackfillRow{Table: spec.table, IssueID: entry.values[0].String, Field: spec.field, Name: name, Action: "unresolved"}
			id, err := r.Resolve(name)
			if err != nil {
				report = append(report, item)
				continue
			}
			item.SeatID, item.Action = id, "unchanged"
			if name != id {
				item.Action = "resolved"
			}
			report = append(report, item)
			if !apply || name == id {
				continue
			}
			entry.values[spec.actorIndex] = sql.NullString{String: id, Valid: true}
			newKey := entry.key
			if spec.derived {
				digest := rowid.Digest(entry.values)
				for ordinal := 0; ; ordinal++ {
					candidate := rowid.New(spec.table, ordinal, digest)
					if !taken[candidate] || candidate == entry.key {
						newKey = candidate
						break
					}
				}
			}
			//nolint:gosec // key/table/field come only from the fixed descriptors
			query := fmt.Sprintf("UPDATE %s SET id = ?, %s = ? WHERE id = ?", spec.table, spec.field)
			if spec.key != "id" {
				query = fmt.Sprintf("UPDATE %s SET `%s` = ? WHERE `%s` = ?", spec.table, spec.field, spec.key)
			}
			args := []any{newKey, id, entry.key}
			if spec.key != "id" {
				args = []any{id, entry.key}
			}
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return report, err
			}
			delete(taken, entry.key)
			taken[newKey] = true
		}
	}
	return report, nil
}
