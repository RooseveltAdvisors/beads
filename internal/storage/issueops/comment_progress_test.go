package issueops

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestCommentProgressExempt_AssigneeScope(t *testing.T) {
	if !CommentProgressExempt(nil) {
		t.Fatal("nil issue should be exempt")
	}
	open := &types.Issue{Status: types.StatusOpen, Assignee: ""}
	if !CommentProgressExempt(open) {
		t.Fatal("empty assignee should be exempt")
	}
	claimed := &types.Issue{Status: types.StatusInProgress, Assignee: "alice"}
	if CommentProgressExempt(claimed) {
		t.Fatal("assignee set should not be exempt")
	}
	event := &types.Issue{Status: types.StatusOpen, Assignee: "alice", IssueType: types.TypeEvent}
	if !CommentProgressExempt(event) {
		t.Fatal("event type should be exempt")
	}
}

func TestHasProgressCommentSince(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	before := []*types.Comment{{CreatedAt: start.Add(-time.Hour), Text: "old"}}
	after := []*types.Comment{{CreatedAt: start.Add(time.Minute), Text: "fresh"}}
	both := []*types.Comment{
		{CreatedAt: start.Add(-time.Hour), Text: "old"},
		{CreatedAt: start, Text: "at start"},
	}

	if hasProgressCommentSince(nil, &start) {
		t.Fatal("no comments: stale")
	}
	if hasProgressCommentSince(before, &start) {
		t.Fatal("only pre-start comments: stale")
	}
	if !hasProgressCommentSince(after, &start) {
		t.Fatal("post-start comment: fresh")
	}
	if !hasProgressCommentSince(both, &start) {
		t.Fatal("comment at started_at counts")
	}
	if !hasProgressCommentSince(before, nil) {
		t.Fatal("any comment counts when no started_at")
	}
	if hasProgressCommentSince(nil, nil) {
		t.Fatal("no comments and no anchor: stale")
	}
}
