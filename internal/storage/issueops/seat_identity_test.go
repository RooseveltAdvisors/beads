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
	workerUUID := "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	if err := r.Register(workerUUID, []string{"worker"}, seats.Endpoint{Session: "s", PaneID: "p"}, time.Now()); err != nil {
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
	if issue.AssigneeID != workerUUID || issue.Assignee != workerUUID || issue.CreatedBy != workerUUID || issue.Owner != workerUUID || issue.Actor != workerUUID {
		t.Fatalf("identity copies drift: %+v", issue)
	}
}

func TestSeatWriteRejectsNonUUIDIdentity(t *testing.T) {
	r := seats.Registry{}
	if err := r.Register("legacy-worker", []string{"legacy"}, seats.Endpoint{Session: "s", PaneID: "p"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx := seats.WithRegistry(context.Background(), r)
	issue := &types.Issue{Assignee: "legacy"}
	if err := ResolveIssueSeats(ctx, issue); err == nil || !strings.Contains(err.Error(), "resolves to non-UUID identity") {
		t.Fatalf("legacy name-keyed assignment accepted in strict mode: %v", err)
	}
	if err := ResolveSeatUpdates(ctx, map[string]any{"assignee": "legacy"}); err == nil || !strings.Contains(err.Error(), "resolves to non-UUID identity") {
		t.Fatalf("legacy name-keyed update accepted in strict mode: %v", err)
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
