package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

var progressCmd = &cobra.Command{
	Use:     "progress",
	GroupID: "issues",
	Short:   "Comment progress helpers (comment.progress_required)",
	Long: `Helpers for the beads-owned comment progress gate.

When comment.progress_required is true (house default), assigned work must show
a progress trail before close: a comment since started_at, or a non-empty
close --reason, or --force-no-comment --reason.

  bd progress check [id...]   report assigned issues that would fail the gate
`,
}

var progressCheckCmd = &cobra.Command{
	Use:           "check [id...]",
	Short:         "List assigned issues missing a fresh progress comment",
	Args:          cobra.MinimumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		CheckReadonly("progress check")
		if !issueops.CommentProgressRequired() {
			if jsonOutput {
				return outputJSON(map[string]any{"enabled": false, "stale": []any{}})
			}
			fmt.Fprintln(os.Stderr, "comment.progress_required is off")
			return nil
		}

		type row struct {
			ID       string `json:"id"`
			Assignee string `json:"assignee,omitempty"`
			Status   string `json:"status,omitempty"`
			Reason   string `json:"reason"`
		}
		var out []row

		for _, rawID := range args {
			result, err := resolveAndGetIssueWithRouting(rootCtx, store, rawID)
			if err != nil {
				if result != nil {
					result.Close()
				}
				return HandleErrorRespectJSON("resolving %s: %v", rawID, err)
			}
			if result == nil || result.Issue == nil {
				if result != nil {
					result.Close()
				}
				continue
			}
			issue := result.Issue
			if issueops.CommentProgressExempt(issue) || issue.Status == types.StatusClosed {
				result.Close()
				continue
			}
			comments, err := result.Store.GetIssueComments(rootCtx, issue.ID)
			result.Close()
			if err != nil {
				return HandleErrorRespectJSON("getting comments: %v", err)
			}
			if progressFresh(issue, comments) {
				continue
			}
			msg := "no progress comments"
			if issue.StartedAt != nil {
				msg = fmt.Sprintf("no progress comment since %s", issue.StartedAt.UTC().Format(time.RFC3339))
			}
			out = append(out, row{
				ID:       issue.ID,
				Assignee: issue.Assignee,
				Status:   string(issue.Status),
				Reason:   msg,
			})
		}

		if jsonOutput {
			return outputJSON(map[string]any{"enabled": true, "stale": out})
		}
		if len(out) == 0 {
			fmt.Println("ok: no stale progress on given ids")
			return nil
		}
		for _, r := range out {
			fmt.Printf("%s\t%s\t%s\t%s\n", r.ID, r.Assignee, r.Status, r.Reason)
		}
		return fmt.Errorf("%d issue(s) missing progress comments", len(out))
	},
}

func progressFresh(issue *types.Issue, comments []*types.Comment) bool {
	if issue == nil {
		return true
	}
	if len(comments) == 0 {
		return false
	}
	if issue.StartedAt == nil {
		return true
	}
	anchor := issue.StartedAt.UTC()
	for _, c := range comments {
		if c == nil {
			continue
		}
		if !c.CreatedAt.UTC().Before(anchor) {
			return true
		}
	}
	return false
}

func init() {
	progressCmd.AddCommand(progressCheckCmd)
	rootCmd.AddCommand(progressCmd)
}
