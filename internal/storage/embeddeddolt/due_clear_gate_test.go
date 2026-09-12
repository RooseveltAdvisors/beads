//go:build cgo

package embeddeddolt_test

import (
	"errors"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// TestDueClearGate_DoltEngine exercises the storage-side gate on clearing a
// due date, on the embedded-Dolt reference backend: while due.required is on,
// a generic update that sets due_at to nil is refused as ErrValidation unless
// it carries issueops.OpDueClearReason, and the accepted clear records that
// reason on the update event. This is the gate every transport reaches — the
// CLI's --force-no-due --reason and the HTTP due_clear_reason both land here.
func TestDueClearGate_DoltEngine(t *testing.T) {
	te := newTestEnv(t, "dueclear")
	ctx := t.Context()

	t.Chdir(t.TempDir())
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	config.Set(issueops.DueRequiredKey, true)
	t.Cleanup(config.ResetForTesting)

	due := time.Now().Add(48 * time.Hour).UTC()
	issue := &types.Issue{
		ID:        "dueclear-1",
		Title:     "keeps its deadline unless told why",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
		DueAt:     &due,
	}
	if err := te.store.CreateIssue(ctx, issue, "actor"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	err := te.store.UpdateIssue(ctx, issue.ID, map[string]interface{}{"due_at": nil}, "actor")
	if err == nil || !errors.Is(err, storage.ErrValidation) {
		t.Fatalf("clearing without a reason: err = %v, want ErrValidation", err)
	}
	kept, err := te.store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if kept.DueAt == nil {
		t.Fatal("a refused clear removed the due date")
	}

	const why = "tracked upstream instead"
	if err := te.store.UpdateIssue(ctx, issue.ID, map[string]interface{}{
		"due_at":                  nil,
		issueops.OpDueClearReason: why,
	}, "actor"); err != nil {
		t.Fatalf("clearing with a reason: %v", err)
	}
	cleared, err := te.store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if cleared.DueAt != nil {
		t.Fatalf("due_at = %v after a reasoned clear, want nil", cleared.DueAt)
	}
	events, err := te.store.GetEvents(ctx, issue.ID, 0)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	found := false
	for _, event := range events {
		if event.EventType == types.EventUpdated && event.Comment != nil && *event.Comment == why {
			found = true
		}
	}
	if !found {
		t.Errorf("no update event carries the reason %q: %+v", why, events)
	}

	// Clearing an already-empty due date is a no-op, not a gated act.
	if err := te.store.UpdateIssue(ctx, issue.ID, map[string]interface{}{"due_at": nil}, "actor"); err != nil {
		t.Fatalf("re-clearing an empty due date must be a no-op, got %v", err)
	}
}
