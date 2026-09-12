//go:build cgo

package embeddeddolt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// runScheduledSweeps runs the lazy time-based sweeps — defer wake and due
// trigger (issueops.RunScheduledSweepsInTx) — in one write transaction before
// a ready-work read. Advisory by contract:
// a ready listing must never fail because the sweep could not run — strict
// --readonly stores skip it silently (the one mode that reaches ErrReadOnly,
// mirroring the tip-metadata write's tolerance), anything else warns. It runs
// OUTSIDE the read connections below because withConn(ctx, false, …) always
// rolls back.
func (s *EmbeddedDoltStore) runScheduledSweeps(ctx context.Context) {
	err := s.runIssueOperationTxWithMessage(ctx, func(tx *sql.Tx) (issueops.ChangedTables, string, error) {
		swept, err := issueops.RunScheduledSweepsInTx(ctx, tx)
		if err != nil {
			return nil, "", err
		}
		if swept.IssueRows() == 0 {
			// Wisp-only sweeps persist with the SQL commit but mint no version
			// commit: wisp tables are dolt_ignored.
			return nil, "", nil
		}
		tables := issueops.ChangedTables{}
		tables.Add("issues", "events")
		return tables, swept.CommitMessage(), nil
	})
	if err != nil && !errors.Is(err, ErrReadOnly) {
		fmt.Fprintf(os.Stderr, "warning: scheduled sweep skipped: %v\n", err)
	}
}

func (s *EmbeddedDoltStore) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	s.runScheduledSweeps(ctx)
	var result []*types.Issue
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetReadyWorkInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

func (s *EmbeddedDoltStore) GetReadyWorkWithCounts(ctx context.Context, filter types.WorkFilter) ([]*types.IssueWithCounts, error) {
	s.runScheduledSweeps(ctx)
	var result []*types.IssueWithCounts
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetReadyWorkWithCountsInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

// CountReadyWork returns the total ready-work count for filter. It is identical
// to len(GetReadyWorkWithCounts(filter with Limit=0)) but sizes the total with
// cheap indexed COUNT(*)s instead of re-running the counts mega-query. Backs the
// storage.ReadyWorkCounter capability.
func (s *EmbeddedDoltStore) CountReadyWork(ctx context.Context, filter types.WorkFilter) (int, error) {
	s.runScheduledSweeps(ctx)
	var n int
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		n, err = issueops.CountReadyWorkInTx(ctx, tx, filter)
		return err
	})
	return n, err
}

func (s *EmbeddedDoltStore) GetMoleculeProgress(ctx context.Context, moleculeID string) (*types.MoleculeProgressStats, error) {
	var result *types.MoleculeProgressStats
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetMoleculeProgressInTx(ctx, tx, moleculeID)
		return err
	})
	return result, err
}
