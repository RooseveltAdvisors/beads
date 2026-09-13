package issueops

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

type fakeActivity struct {
	issues   []*types.Issue
	comments map[string][]*types.Comment
}

func (f fakeActivity) SearchIssues(context.Context, string, types.IssueFilter) ([]*types.Issue, error) {
	return f.issues, nil
}
func (f fakeActivity) GetIssueComments(_ context.Context, id string) ([]*types.Comment, error) {
	return f.comments[id], nil
}

func TestFindStaleClaims(t *testing.T) {
	now := time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)
	start := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)
	issues := []*types.Issue{
		{ID: "a", Title: "zombie", Status: types.StatusInProgress, Assignee: "wiseman", StartedAt: &start},
		{ID: "b", Title: "fresh comment", Status: types.StatusInProgress, Assignee: "wiseman", StartedAt: &start},
		{ID: "c", Title: "unassigned", Status: types.StatusInProgress, Assignee: "", StartedAt: &start},
	}
	comments := map[string][]*types.Comment{
		"b": {{CreatedAt: fresh, Text: "still going"}},
	}
	got, err := FindStaleClaims(context.Background(), fakeActivity{issues, comments}, 12*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("want only zombie a, got %+v", got)
	}
}
