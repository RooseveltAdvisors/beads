package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/dedup"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
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
	BlockedParent string  `json:"blocked_parent,omitempty"`
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
		if b.BlockedParent != "" {
			fmt.Fprintf(os.Stderr, "  %s skipped: parent %s blocked as duplicate of %s\n",
				b.ID, b.BlockedParent, b.ConflictID)
			continue
		}
		pct := int(math.Round(b.Similarity * 100))
		fmt.Fprintf(os.Stderr, "  %s conflicts with %s (%s, %d%% similar)\n",
			b.ID, b.ConflictID, b.ConflictTitle, pct)
	}
	return &exitError{Code: 1}
}

// filterGraphPlan removes graph nodes that duplicate an active issue and every
// node whose parent chain reaches a removed node (so no child is created as an
// orphan). Edges and inline deps that pointed at a duplicate node are remapped
// onto the active issue it duplicates, so surviving nodes keep their ordering;
// those touching a removed child, or left with no new node on either end, are
// dropped. It returns the removed nodes as blocked items.
func filterGraphPlan(ctx context.Context, s storage.DoltStorage, plan *GraphApplyPlan, opts GraphApplyOptions) ([]blockedJSONItem, error) {
	if !isDedupCheckEnabled() || opts.AllowDuplicate || opts.Force {
		return nil, nil
	}
	if s == nil && uowProvider == nil {
		return nil, nil
	}
	activeIssues, err := queryActiveIssues(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("duplicate check: fetch active issues: %w", err)
	}
	if len(activeIssues) == 0 {
		return nil, nil
	}
	label := func(node GraphApplyNode) string {
		if node.ID != "" {
			return node.ID
		}
		return node.Key
	}
	matcher := dedup.NewMatcher(activeIssues)
	var blocked []blockedJSONItem
	blockedByKey := make(map[string]blockedJSONItem)
	for _, node := range plan.Nodes {
		if conflict := matcher.FindConflict(node.Title); conflict != nil {
			item := blockedJSONItem{
				ID:            label(node),
				ConflictID:    conflict.ConflictIssue.ID,
				ConflictTitle: conflict.ConflictIssue.Title,
				Similarity:    conflict.Similarity,
			}
			blocked = append(blocked, item)
			blockedByKey[node.Key] = item
		}
	}
	if len(blocked) == 0 {
		return nil, nil
	}
	for changed := true; changed; {
		changed = false
		for _, node := range plan.Nodes {
			if _, done := blockedByKey[node.Key]; done {
				continue
			}
			parentKey := node.effectiveParentKey()
			if parentKey == "" {
				continue
			}
			parent, ok := blockedByKey[parentKey]
			if !ok {
				continue
			}
			item := blockedJSONItem{
				ID:            label(node),
				ConflictID:    parent.ConflictID,
				ConflictTitle: parent.ConflictTitle,
				Similarity:    parent.Similarity,
				BlockedParent: parent.ID,
			}
			blocked = append(blocked, item)
			blockedByKey[node.Key] = item
			changed = true
		}
	}
	isBlocked := func(key string) bool {
		if key == "" {
			return false
		}
		_, ok := blockedByKey[key]
		return ok
	}
	conflictFor := func(key string) (string, bool) {
		item, ok := blockedByKey[key]
		if !ok || key == "" || item.BlockedParent != "" {
			return "", false
		}
		return item.ConflictID, true
	}
	var nodes []GraphApplyNode
	for _, node := range plan.Nodes {
		if isBlocked(node.Key) {
			continue
		}
		var deps []GraphApplyNodeDep
		for _, dep := range node.Deps {
			if id, ok := conflictFor(dep.Target); ok {
				dep.Target = id
			} else if isBlocked(dep.Target) {
				continue
			}
			deps = append(deps, dep)
		}
		node.Deps = deps
		nodes = append(nodes, node)
	}
	var edges []GraphApplyEdge
	for _, edge := range plan.Edges {
		remapped := false
		if id, ok := conflictFor(edge.FromKey); ok {
			edge.FromKey, edge.FromID, remapped = "", id, true
		}
		if id, ok := conflictFor(edge.ToKey); ok {
			edge.ToKey, edge.ToID, remapped = "", id, true
		}
		if id, ok := conflictFor(edge.SpawnerKey); ok {
			edge.SpawnerKey, edge.SpawnerID = "", id
		}
		if isBlocked(edge.FromKey) || isBlocked(edge.ToKey) || isBlocked(edge.SpawnerKey) {
			continue
		}
		if remapped && edge.FromKey == "" && edge.ToKey == "" {
			continue
		}
		edges = append(edges, edge)
	}
	plan.Nodes = nodes
	plan.Edges = edges
	return blocked, nil
}

// reportBlockedGraph prints the nodes a graph apply did create (human mode)
// and then reports the blocked nodes through reportBlockedBatch.
func reportBlockedGraph(blocked []blockedJSONItem, ids map[string]string, isJSON bool) error {
	keys := make([]string, 0, len(ids))
	for key := range ids {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	createdIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		createdIDs = append(createdIDs, ids[key])
	}
	if !isJSON && len(keys) > 0 {
		fmt.Printf("Created %d issues\n", len(keys))
		for _, key := range keys {
			fmt.Printf("  %s -> %s\n", key, ids[key])
		}
	}
	return reportBlockedBatch(blocked, createdIDs, isJSON)
}

// graphCreatedIssues pairs each created graph node's ID with its title for
// the post-insert re-check.
func graphCreatedIssues(nodes []GraphApplyNode, ids map[string]string) []*types.Issue {
	issues := make([]*types.Issue, 0, len(ids))
	for _, node := range nodes {
		if id, ok := ids[node.Key]; ok {
			issues = append(issues, &types.Issue{ID: id, Title: node.Title})
		}
	}
	return issues
}

// noteDuplicateCollisions is the post-insert re-check for the window the
// pre-create check cannot close: another seat creating the same title between
// that check's read and this create's write. Each created issue whose exact
// title matches another active issue gets a comment naming the collision.
// Neither side is deleted.
func noteDuplicateCollisions(ctx context.Context, s storage.DoltStorage, created []*types.Issue, bypass bool) {
	if !isDedupCheckEnabled() || bypass || len(created) == 0 {
		return
	}
	if s == nil && uowProvider == nil {
		return
	}
	activeIssues, err := queryActiveIssues(ctx, s)
	if err != nil {
		WarnError("duplicate re-check: fetch active issues: %v", err)
		return
	}
	createdIDs := make(map[string]bool, len(created))
	for _, issue := range created {
		createdIDs[issue.ID] = true
	}
	for _, issue := range created {
		want := dedup.NormalizeTitle(issue.Title)
		for _, other := range activeIssues {
			if createdIDs[other.ID] || dedup.NormalizeTitle(other.Title) != want {
				continue
			}
			text := fmt.Sprintf("duplicate-title collision: active issue %s (created %s) has the same title; "+
				"this issue was written past the pre-create duplicate check, likely by a concurrent create. "+
				"Neither issue was deleted. A DB-level unique constraint would be the true fix (out of scope here).",
				other.ID, other.CreatedAt.UTC().Format(time.RFC3339))
			if err := addCollisionComment(ctx, s, issue.ID, text); err != nil {
				WarnError("duplicate re-check: comment on %s: %v", issue.ID, err)
			}
			break
		}
	}
}

func addCollisionComment(ctx context.Context, s storage.DoltStorage, issueID, text string) error {
	if s != nil {
		_, err := addCommentDirect(ctx, s, issueID, actor, text)
		return err
	}
	commenter, err := proxiedCommenter()
	if err != nil {
		return err
	}
	_, err = commenter.AddComment(ctx, issueops.AddCommentRequest{Author: actor, IssueID: issueID, Text: text})
	return err
}
