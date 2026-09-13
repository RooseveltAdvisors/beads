package issueops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

// Stale claim clock (beads-owned). Finds assigned in_progress work that has
// gone quiet without a close, so notify can enqueue KindStaleClaim and teach
// the finish line (bd comment / bd close --reason).

const (
	// StaleClaimAfterKey is how long assigned in_progress may stay quiet.
	// Duration string (e.g. "12h", "24h"). Empty or "0" disables the clock.
	StaleClaimAfterKey = "comment.stale_claim_after"
)

// StaleClaimAfter returns the quiet threshold, or 0 if the clock is off.
func StaleClaimAfter() time.Duration {
	raw := strings.TrimSpace(config.GetString(StaleClaimAfterKey))
	if raw == "" || raw == "0" || strings.EqualFold(raw, "off") || strings.EqualFold(raw, "false") {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// StaleClaim is one assigned in_progress bead quiet longer than the threshold.
type StaleClaim struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Assignee   string    `json:"assignee"`
	Status     string    `json:"status"`
	QuietSince time.Time `json:"quiet_since"`
	QuietFor   string    `json:"quiet_for"`
	Reason     string    `json:"reason"`
}

// IssueActivity is the minimal read surface the stale clock needs.
type IssueActivity interface {
	SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error)
	GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error)
}

// FindStaleClaims lists assignee-set in_progress issues whose last progress
// (comment or started_at) is older than after.
func FindStaleClaims(ctx context.Context, s IssueActivity, after time.Duration, now time.Time) ([]StaleClaim, error) {
	if s == nil || after <= 0 {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	st := types.StatusInProgress
	issues, err := s.SearchIssues(ctx, "", types.IssueFilter{Status: &st, Limit: 500})
	if err != nil {
		return nil, fmt.Errorf("stale claim: search: %w", err)
	}
	var out []StaleClaim
	for _, issue := range issues {
		if issue == nil || CommentProgressExempt(issue) {
			continue
		}
		anchor, src, err := progressAnchor(ctx, s, issue)
		if err != nil {
			return nil, err
		}
		if anchor.IsZero() {
			// Never started and no comments: use updated_at as weak anchor.
			if issue.UpdatedAt.IsZero() {
				continue
			}
			anchor = issue.UpdatedAt.UTC()
			src = "updated_at"
		}
		if now.Sub(anchor) < after {
			continue
		}
		out = append(out, StaleClaim{
			ID:         issue.ID,
			Title:      issue.Title,
			Assignee:   issue.Assignee,
			Status:     string(issue.Status),
			QuietSince: anchor,
			QuietFor:   now.Sub(anchor).Truncate(time.Minute).String(),
			Reason:     fmt.Sprintf("no progress since %s (%s)", src, anchor.Format(time.RFC3339)),
		})
	}
	return out, nil
}

func progressAnchor(ctx context.Context, s IssueActivity, issue *types.Issue) (time.Time, string, error) {
	var best time.Time
	src := ""
	if issue.StartedAt != nil && !issue.StartedAt.IsZero() {
		best = issue.StartedAt.UTC()
		src = "started_at"
	}
	comments, err := s.GetIssueComments(ctx, issue.ID)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("stale claim: comments %s: %w", issue.ID, err)
	}
	for _, c := range comments {
		if c == nil || c.CreatedAt.IsZero() {
			continue
		}
		t := c.CreatedAt.UTC()
		if best.IsZero() || t.After(best) {
			best = t
			src = "comment"
		}
	}
	return best, src, nil
}
