package dolt

import (
	"context"
	"database/sql"

	"github.com/steveyegge/beads/internal/seats"
	"github.com/steveyegge/beads/internal/storage/issueops"
)

func (s *DoltStore) BackfillSeatIDs(ctx context.Context, r seats.Registry, apply bool) ([]seats.BackfillRow, error) {
	var report []seats.BackfillRow
	fn := func(tx *sql.Tx) error {
		var err error
		report, err = issueops.BackfillSeatIDsInTx(ctx, tx, r, apply)
		return err
	}
	var err error
	if apply {
		err = s.withWriteTx(ctx, fn)
	} else {
		err = s.withReadTx(ctx, fn)
	}
	return report, err
}
