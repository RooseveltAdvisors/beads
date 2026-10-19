//go:build cgo

package embeddeddolt

import (
	"context"
	"database/sql"
	"github.com/steveyegge/beads/internal/seats"
	"github.com/steveyegge/beads/internal/storage/issueops"
)

func (s *EmbeddedDoltStore) BackfillSeatIDs(ctx context.Context, r seats.Registry, apply bool) ([]seats.BackfillRow, error) {
	var report []seats.BackfillRow
	err := s.withConn(ctx, apply, func(tx *sql.Tx) error {
		var err error
		report, err = issueops.BackfillSeatIDsInTx(ctx, tx, r, apply)
		return err
	})
	return report, err
}
