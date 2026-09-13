package issueops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// Comment progress policy (beads-owned, like due/notify - not firstmate).
//
// Locked design (captain 2026-09-13):
//   - strength: gate close (refuse done-class close without fresh progress)
//   - freshness: comment since claim/start (started_at), or close --reason
//   - scope: assignee set only
//   - default: ON in this house fork (upstream contrib should default OFF)
const (
	// CommentProgressRequiredKey turns the close gate on.
	CommentProgressRequiredKey = "comment.progress_required"
	// CommentProgressScopeKey is reserved for future scopes; house uses assignee.
	CommentProgressScopeKey = "comment.progress_scope"
)

// CommentProgressRequired reports whether this workspace gates closes on
// progress comments.
func CommentProgressRequired() bool {
	return config.GetBool(CommentProgressRequiredKey)
}

// CommentProgressExempt is true when the issue is outside the gate (no assignee,
// or not "work" in the same sense as due.required exemptions).
func CommentProgressExempt(issue *types.Issue) bool {
	if issue == nil {
		return true
	}
	if strings.TrimSpace(issue.Assignee) == "" {
		return true // scope: assignee-set only
	}
	// Same non-work classes as due.required.
	if DueRequiredExempt(issue) {
		return true
	}
	return false
}

// ValidateCommentProgressForClose refuses a close that would end assigned work
// with no progress trail since the work started.
//
// Satisfied by any of:
//   - a structured comment with created_at >= started_at (or any comment if
//     started_at was never set)
//   - a non-empty close reason (counts as the final progress note)
//   - forceNoComment with a non-empty forceReason (escape hatch)
func ValidateCommentProgressForClose(ctx context.Context, tx DBTX, issueID, reason string, forceNoComment bool, forceReason string) error {
	if !CommentProgressRequired() {
		return nil
	}
	if forceNoComment {
		if strings.TrimSpace(forceReason) == "" {
			return fmt.Errorf("%w: --force-no-comment requires a non-empty --reason explaining why progress was skipped", storage.ErrValidation)
		}
		return nil
	}
	// Close reason is the final progress note.
	if strings.TrimSpace(reason) != "" {
		return nil
	}

	issue, err := GetIssueInTx(ctx, tx, issueID)
	if err != nil {
		return err
	}
	if CommentProgressExempt(issue) {
		return nil
	}
	// Already closed: re-close is a no-op path; do not demand comments.
	if issue.Status == types.StatusClosed {
		return nil
	}

	comments, err := GetIssueCommentsInTx(ctx, tx, issueID)
	if err != nil {
		return fmt.Errorf("comment progress: list comments: %w", err)
	}

	var anchor *time.Time
	if issue.StartedAt != nil {
		t := issue.StartedAt.UTC()
		anchor = &t
	}

	if hasProgressCommentSince(comments, anchor) {
		return nil
	}

	if anchor != nil {
		return fmt.Errorf("%w: comment.progress_required: issue %s (assignee %q) has no progress comment since work started at %s — run `bd comment %s \"…\"` or pass --reason on close, or --force-no-comment --reason \"…\"",
			storage.ErrValidation, issueID, issue.Assignee, anchor.Format(time.RFC3339), issueID)
	}
	return fmt.Errorf("%w: comment.progress_required: issue %s (assignee %q) has no progress comments — run `bd comment %s \"…\"` or pass --reason on close, or --force-no-comment --reason \"…\"",
		storage.ErrValidation, issueID, issue.Assignee, issueID)
}

func hasProgressCommentSince(comments []*types.Comment, anchor *time.Time) bool {
	if len(comments) == 0 {
		return false
	}
	if anchor == nil {
		return true // any comment counts when we never recorded started_at
	}
	for _, c := range comments {
		if c == nil {
			continue
		}
		t := c.CreatedAt.UTC()
		if t.IsZero() || !t.Before(*anchor) {
			return true
		}
	}
	return false
}

// ProgressStale describes an assigned open/in_progress issue with no fresh comment.
type ProgressStale struct {
	ID       string `json:"id"`
	Assignee string `json:"assignee"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
}

// CheckCommentProgressInTx lists assigned issues that would fail the gate if closed now
// without a reason (advisory surface for bd progress check / clocks).
func CheckCommentProgressInTx(ctx context.Context, tx DBTX, issueIDs []string) ([]ProgressStale, error) {
	if !CommentProgressRequired() {
		return nil, nil
	}
	var out []ProgressStale
	for _, id := range issueIDs {
		issue, err := GetIssueInTx(ctx, tx, id)
		if err != nil || issue == nil {
			continue
		}
		if CommentProgressExempt(issue) || issue.Status == types.StatusClosed {
			continue
		}
		// Only flag work that looks claimed.
		if issue.Status != types.StatusInProgress && issue.Status != types.StatusOpen {
			continue
		}
		err = ValidateCommentProgressForClose(ctx, tx, id, "", false, "")
		if err != nil {
			out = append(out, ProgressStale{
				ID:       id,
				Assignee: issue.Assignee,
				Status:   string(issue.Status),
				Reason:   err.Error(),
			})
		}
	}
	return out, nil
}
