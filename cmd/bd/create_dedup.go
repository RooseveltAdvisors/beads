package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/dedup"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// isDedupCheckEnabled reports whether duplicate checking is enabled via config.
// Defaults to true when unset.
func isDedupCheckEnabled() bool {
	if config.IsSet("create.dedup-check") {
		return config.GetBool("create.dedup-check")
	}
	return true
}

// activeDedupStatuses defines the active-only status set for duplicate detection.
var activeDedupStatuses = []types.Status{
	types.StatusOpen,
	types.StatusInProgress,
	types.StatusBlocked,
}

// queryActiveIssues reads only active issues for duplicate checking.
// The query is bounded and indexed on status. Closed, archived, and history
// beads are strictly ignored.
func queryActiveIssues(ctx context.Context, s storage.DoltStorage) ([]*types.Issue, error) {
	if s != nil {
		filter := types.IssueFilter{ //nolint:forbidigo // active-only pre-creation duplicate check query
			Statuses: activeDedupStatuses,
		}
		return s.SearchIssues(ctx, "", filter)
	}
	if uowProvider != nil {
		uw, err := uowProvider.NewUOW(ctx)
		if err != nil {
			return nil, err
		}
		defer uw.Close(ctx)
		filter := types.IssueFilter{ //nolint:forbidigo // active-only pre-creation duplicate check query
			Statuses: activeDedupStatuses,
		}
		page, err := uw.IssueUseCase().SearchIssuesWithCounts(ctx, "", filter)
		if err != nil {
			return nil, err
		}
		issues := make([]*types.Issue, 0, len(page.Items))
		for _, item := range page.Items {
			if item != nil && item.Issue != nil {
				issues = append(issues, item.Issue)
			}
		}
		return issues, nil
	}
	return nil, nil
}

// formatDuplicateBlockError renders the duplicate block error following firstmate
// design ruling 5: conflict first, then bypass flags, then config key, in that order,
// fitting on one screen.
func formatDuplicateBlockError(conflict *dedup.ConflictResult) string {
	issue := conflict.ConflictIssue
	assignee := issue.Assignee
	if assignee == "" {
		assignee = "(unassigned)"
	}

	pct := int(math.Round(conflict.Similarity * 100))

	var b strings.Builder
	// 1. Conflict first
	fmt.Fprintf(&b, "blocked: potential duplicate of active issue %s (%d%% similar)\n", issue.ID, pct)
	fmt.Fprintf(&b, "  Conflict:\n")
	fmt.Fprintf(&b, "    ID:       %s\n", issue.ID)
	fmt.Fprintf(&b, "    Title:    %s\n", issue.Title)
	fmt.Fprintf(&b, "    Status:   %s\n", issue.Status)
	fmt.Fprintf(&b, "    Priority: P%d\n", issue.Priority)
	fmt.Fprintf(&b, "    Assignee: %s\n", assignee)
	fmt.Fprintf(&b, "  Action:\n")
	fmt.Fprintf(&b, "    View:     bd show %s\n", issue.ID)
	fmt.Fprintf(&b, "    Claim:    bd update %s --claim\n", issue.ID)
	fmt.Fprintf(&b, "\n")
	// 2. Bypass flags second
	fmt.Fprintf(&b, "To bypass this check:\n")
	fmt.Fprintf(&b, "  --allow-duplicate    Allow creating this duplicate issue\n")
	fmt.Fprintf(&b, "  --force              Force creation\n")
	fmt.Fprintf(&b, "\n")
	// 3. Config key third
	fmt.Fprintf(&b, "Configuration:\n")
	fmt.Fprintf(&b, "  create.dedup-check (default true)\n")
	fmt.Fprintf(&b, "  Disable check:       bd config set create.dedup-check false")

	return b.String()
}

// blockedJSONItem represents a blocked duplicate item for structured JSON output.
type blockedJSONItem struct {
	ID            string  `json:"id,omitempty"`
	ConflictID    string  `json:"conflict_id"`
	ConflictTitle string  `json:"conflict_title"`
	Similarity    float64 `json:"similarity"`
}

// checkDuplicateSingle runs duplicate detection for a single issue creation.
func checkDuplicateSingle(ctx context.Context, s storage.DoltStorage, title, explicitID string, allowDuplicate, force bool) error {
	if !isDedupCheckEnabled() || allowDuplicate || force {
		return nil
	}
	if s == nil && uowProvider == nil {
		return nil
	}

	activeIssues, err := queryActiveIssues(ctx, s)
	if err != nil {
		return fmt.Errorf("duplicate check: fetch active issues: %w", err)
	}
	if len(activeIssues) == 0 {
		return nil
	}

	matcher := dedup.NewMatcher(activeIssues)
	conflict := matcher.FindConflict(title)
	if conflict == nil {
		return nil
	}

	if jsonOutput {
		item := blockedJSONItem{
			ID:            explicitID,
			ConflictID:    conflict.ConflictIssue.ID,
			ConflictTitle: conflict.ConflictIssue.Title,
			Similarity:    conflict.Similarity,
		}
		res := map[string]interface{}{
			"error": fmt.Sprintf("blocked: potential duplicate of active issue %s", conflict.ConflictIssue.ID),
			"blocked": []blockedJSONItem{
				item,
			},
		}
		_ = outputJSON(res)
		return &exitError{Code: 1}
	}

	return HandleError("%s", formatDuplicateBlockError(conflict))
}

// filterMarkdownTemplates partitions markdown templates into allowed and blocked sets
// according to active-issue similarity rules.
func filterMarkdownTemplates(ctx context.Context, s storage.DoltStorage, templates []*IssueTemplate, in createInput) ([]*IssueTemplate, []blockedJSONItem, error) {
	if !isDedupCheckEnabled() || in.allowDuplicate || in.force {
		return templates, nil, nil
	}
	if s == nil && uowProvider == nil {
		return templates, nil, nil
	}
	activeIssues, err := queryActiveIssues(ctx, s)
	if err != nil {
		return nil, nil, fmt.Errorf("duplicate check: fetch active issues: %w", err)
	}
	if len(activeIssues) == 0 {
		return templates, nil, nil
	}
	matcher := dedup.NewMatcher(activeIssues)
	var allowed []*IssueTemplate
	var blocked []blockedJSONItem
	for _, tmpl := range templates {
		if conflict := matcher.FindConflict(tmpl.Title); conflict != nil {
			blocked = append(blocked, blockedJSONItem{
				ID:            tmpl.Title,
				ConflictID:    conflict.ConflictIssue.ID,
				ConflictTitle: conflict.ConflictIssue.Title,
				Similarity:    conflict.Similarity,
			})
		} else {
			allowed = append(allowed, tmpl)
		}
	}
	return allowed, blocked, nil
}

// reportBlockedBatch reports blocked duplicate items for batch creation operations
// with fail-closed semantics. Exits non-zero (Code 1).
func reportBlockedBatch(blocked []blockedJSONItem, createdIDs []string, isJSON bool) error {
	if isJSON {
		if createdIDs == nil {
			createdIDs = []string{}
		}
		res := map[string]interface{}{
			"created": createdIDs,
			"blocked": blocked,
		}
		_ = outputJSON(res)
		return &exitError{Code: 1}
	}
	fmt.Fprintf(os.Stderr, "Blocked %d duplicate issue(s):\n", len(blocked))
	for _, b := range blocked {
		pct := int(math.Round(b.Similarity * 100))
		fmt.Fprintf(os.Stderr, "  %s conflicts with %s (%s, %d%% similar)\n",
			b.ID, b.ConflictID, b.ConflictTitle, pct)
	}
	return &exitError{Code: 1}
}
