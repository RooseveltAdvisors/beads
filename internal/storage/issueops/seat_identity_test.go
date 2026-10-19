package issueops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/seats"
	"github.com/steveyegge/beads/internal/types"
)

func TestSeatWriteRejectsUnknownName(t *testing.T) {
	r := seats.Registry{}
	if err := r.Register("seat:worker", []string{"worker"}, seats.Endpoint{Session: "s", PaneID: "p"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx := seats.WithRegistry(context.Background(), r)
	issue := &types.Issue{Assignee: "arcs-fm"}
	if err := ResolveIssueSeats(ctx, issue); err == nil || !strings.Contains(err.Error(), `unknown seat "arcs-fm"`) {
		t.Fatalf("unknown assignment accepted: %v", err)
	}
	if err := ResolveSeatUpdates(ctx, map[string]any{"assignee": "arcs-fm"}); err == nil {
		t.Fatal("unknown update accepted")
	}
	issue = &types.Issue{Assignee: "worker", CreatedBy: "worker", Owner: "worker", Actor: "worker"}
	if err := ResolveIssueSeats(ctx, issue); err != nil {
		t.Fatal(err)
	}
	if issue.AssigneeID != "seat:worker" || issue.Assignee != "seat:worker" || issue.CreatedBy != "seat:worker" || issue.Owner != "seat:worker" || issue.Actor != "seat:worker" {
		t.Fatalf("identity copies drift: %+v", issue)
	}
}

func TestLegacySeatWritesKeepAssigneeIDEmpty(t *testing.T) {
	ctx := seats.WithRegistry(context.Background(), seats.Registry{})
	issue := &types.Issue{Assignee: "alice", AssigneeID: "jon"}
	if err := ResolveIssueSeats(ctx, issue); err != nil {
		t.Fatal(err)
	}
	if issue.AssigneeID != "" || issue.Assignee != "alice" {
		t.Fatalf("legacy insert kept a diverging assignee_id: %+v", issue)
	}
	updates := map[string]any{"assignee": "alice"}
	if err := ResolveSeatUpdates(ctx, updates); err != nil {
		t.Fatal(err)
	}
	if id, ok := updates["assignee_id"]; !ok || id != "" {
		t.Fatalf("legacy reassignment left assignee_id stale: %+v", updates)
	}
	if got := SeatAssigneeID(ctx, "jon"); got != "" {
		t.Fatalf("legacy claim would write assignee_id %q", got)
	}
}
