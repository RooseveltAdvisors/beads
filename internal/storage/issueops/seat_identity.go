package issueops

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/seats"
	"github.com/steveyegge/beads/internal/types"
	publicops "github.com/steveyegge/beads/issueops"
)

// ResolveIssueSeats is the shared insert boundary, including imports and moves.
// Persist identities, never mutable aliases; the CLI projects display names.
func ResolveIssueSeats(ctx context.Context, issue *types.Issue) error {
	if err := seats.ContextError(ctx); err != nil {
		return err
	}
	r, strict := seats.FromContext(ctx)
	if !strict || !seats.Strict(r) {
		issue.AssigneeID = ""
		return nil
	}
	assignment := issue.AssigneeID
	if assignment == "" {
		assignment = issue.Assignee
	}
	id, err := r.Resolve(assignment)
	if err != nil {
		return err
	}
	if issue.AssigneeID != "" {
		if _, exists := r[issue.AssigneeID]; !exists {
			return fmt.Errorf("unknown assignee_id %q", issue.AssigneeID)
		}
	}
	issue.AssigneeID, issue.Assignee = id, id
	for _, field := range []*string{&issue.CreatedBy, &issue.Owner, &issue.Actor} {
		value, err := r.Resolve(*field)
		if err != nil {
			return err
		}
		*field = value
	}
	return nil
}

func ResolveUpdateSeatRequest(ctx context.Context, request *publicops.UpdateRequest) error {
	var err error
	request.Actor, err = seats.ResolveContext(ctx, request.Actor)
	if err != nil {
		return err
	}
	if request.Patch.Assignee.Set {
		request.Patch.Assignee.Value, err = seats.ResolveContext(ctx, request.Patch.Assignee.Value)
		if err != nil {
			return err
		}
	}
	if request.Patch.Owner.Set {
		request.Patch.Owner.Value, err = seats.ResolveContext(ctx, request.Patch.Owner.Value)
		if err != nil {
			return err
		}
	}
	if request.ExpectedAssignee != nil {
		id, err := seats.ResolveContext(ctx, *request.ExpectedAssignee)
		if err != nil {
			return err
		}
		request.ExpectedAssignee = &id
	}
	return nil
}

func ResolveSeatUpdates(ctx context.Context, updates map[string]interface{}) error {
	if err := seats.ContextError(ctx); err != nil {
		return err
	}
	r, strict := seats.FromContext(ctx)
	if !strict || !seats.Strict(r) {
		if _, ok := updates["assignee"]; ok {
			updates["assignee_id"] = ""
		}
		return nil
	}
	if _, supplied := updates["assignee_id"]; supplied {
		return fmt.Errorf("assignee_id is system maintained: update --assignee with a registered seat")
	}
	for _, key := range []string{"assignee", "owner", "created_by", "actor"} {
		raw, ok := updates[key]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			return fmt.Errorf("%s must be a seat name or id", key)
		}
		id, err := r.Resolve(value)
		if err != nil {
			return err
		}
		updates[key] = id
		if key == "assignee" {
			updates["assignee_id"] = id
		}
	}
	return nil
}

// SeatAssigneeID is the assignee_id written beside assignee: the seat ID in a
// strict workspace, empty in a legacy one so the two columns never diverge.
func SeatAssigneeID(ctx context.Context, assignee string) string {
	if r, ok := seats.FromContext(ctx); ok && seats.Strict(r) {
		return assignee
	}
	return ""
}
