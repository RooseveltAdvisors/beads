package issueops

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

// The queue cap is the structural answer to a backlog that reached 2,000+
// beads: intake outran closing for weeks, and nothing said stop. Two ceilings
// make the stop a property of the store rather than of anyone's vigilance:
//
//   - create.max-open-per-assignee: one seat cannot hold the fleet's work.
//   - create.max-open-total: a store cannot grow past a workable queue.
//
// Both are config keys (`bd config set create.max-open-per-assignee 100`),
// both count only WORK beads (open, in_progress, blocked - deferred is on ice
// and closed is finished), and both are bypassed deliberately, never
// silently: --force on the CLI, SkipQueueCap on a batch (bd import), the same
// shape SkipDueRequired set for the mandatory-due invariant.
const (
	// QueueCapPerAssigneeKey bounds one assignee's open work.
	QueueCapPerAssigneeKey = "create.max-open-per-assignee"
	// QueueCapTotalKey bounds the store's open work.
	QueueCapTotalKey = "create.max-open-total"
)

// QueueCapPerAssignee returns the per-assignee ceiling (0 = off).
func QueueCapPerAssignee() int { return config.GetInt(QueueCapPerAssigneeKey) }

// QueueCapTotal returns the store-wide ceiling (0 = off).
func QueueCapTotal() int { return config.GetInt(QueueCapTotalKey) }

// ValidateQueueCap refuses a create that would push a queue past its ceiling.
// It is called from CreateIssueInTxWithResult - the one body every non-public
// create reaches - and from the public create validation, exactly where
// ValidateDueRequired runs, so every create surface answers to it.
//
// Exempt rows are the same classes the mandatory-due invariant exempts
// (events, wisps, templates, federated mirrors): none of them is work a seat
// is meant to finish, so none of them counts against a work queue. Imports
// set SkipQueueCap at the call site, because an imported row's queue position
// is a property of the caller restoring it.
func ValidateQueueCap(ctx context.Context, tx DBTX, bc *BatchContext, issue *types.Issue) error {
	if bc != nil && bc.Opts.SkipQueueCap {
		return nil
	}
	if DueRequiredExempt(issue) {
		return nil
	}
	table, _ := TableRouting(issue)
	assignee := strings.TrimSpace(issue.Assignee)

	if per := QueueCapPerAssignee(); per > 0 {
		n, err := countOpenWorkInTx(ctx, tx, table, assignee)
		if err != nil {
			return fmt.Errorf("queue cap: %w", err)
		}
		if n >= per {
			return fmt.Errorf(
				"queue cap: %s already holds %d open beads (create.max-open-per-assignee=%d); "+
					"close what is done or on ice first, or raise the ceiling "+
					"(`bd config set %s <n>`, 0 turns it off); `bd import` skips it",
				displayAssignee(assignee), n, per, QueueCapPerAssigneeKey)
		}
	}

	if total := QueueCapTotal(); total > 0 {
		n, err := countOpenWorkInTx(ctx, tx, table, "")
		if err != nil {
			return fmt.Errorf("queue cap: %w", err)
		}
		if n >= total {
			return fmt.Errorf(
				"queue cap: this store already holds %d open beads (create.max-open-total=%d); "+
					"the queue stops being a queue past what anyone can work - close what is done "+
					"or on ice, or raise the ceiling (`bd config set %s <n>`, 0 turns it off)",
				n, total, QueueCapTotalKey)
		}
	}
	return nil
}

// countOpenWorkInTx counts work beads (open, in_progress, blocked) on the
// given table. An empty assignee counts the whole table, which is how the
// store-wide ceiling reads.
//
// Deferred is deliberately not counted: it is the on-ice state, and a store
// whose on-ice rows filled the ceiling would block new work for no reason.
func countOpenWorkInTx(ctx context.Context, tx DBTX, table, assignee string) (int, error) {
	var n int
	var err error
	if assignee == "" {
		err = tx.QueryRowContext(ctx, fmt.Sprintf(
			`SELECT COUNT(*) FROM %s WHERE status IN ('open','in_progress','blocked')`, table),
		).Scan(&n)
	} else {
		err = tx.QueryRowContext(ctx, fmt.Sprintf(
			`SELECT COUNT(*) FROM %s WHERE assignee = ? AND status IN ('open','in_progress','blocked')`, table),
			assignee).Scan(&n)
	}
	return n, err
}

func displayAssignee(assignee string) string {
	if assignee == "" {
		return "the unassigned queue"
	}
	return assignee
}
