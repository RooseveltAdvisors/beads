//go:build cgo

package embeddeddolt_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// validRecurrenceMetadata is a fully-formed recurrence contract for jr-voice.
const validRecurrenceMetadata = `{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York"}`

// TestRecurringIssueOwnerSurvivesReclaimAndUnclaim pins the lifecycle half of
// the recurrence contract: claim → expired-lease reclaim → claim by a
// different actor → unclaim must never move the canonical owner off the
// schedule, on the shared issueops path both backends run.
func TestRecurringIssueOwnerSurvivesReclaimAndUnclaim(t *testing.T) {
	skipUnlessEmbeddedDolt(t)

	te := newTestEnv(t, "recown")
	ctx := t.Context()

	issue := &types.Issue{
		ID:        "recown-1",
		Title:     "Publish daily jonroosevelt.com blog post",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Assignee:  "jr-voice",
		Metadata:  json.RawMessage(validRecurrenceMetadata),
	}
	if err := te.store.CreateIssue(ctx, issue, "seeder"); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	shortLease := issueops.WithLeaseTTL(ctx, time.Second)

	if err := te.store.ClaimIssue(shortLease, "recown-1", "jr_voice"); err != nil {
		t.Fatalf("ClaimIssue(jr_voice): %v", err)
	}
	got, err := te.store.GetIssue(ctx, "recown-1")
	if err != nil {
		t.Fatalf("GetIssue after claim: %v", err)
	}
	if got.Assignee != "jr-voice" || got.Status != types.StatusInProgress {
		t.Fatalf("after claim assignee/status = %q/%q, want jr-voice/in_progress", got.Assignee, got.Status)
	}

	time.Sleep(2500 * time.Millisecond)
	reclaimed, err := te.store.ReclaimExpiredLeases(ctx, 0, types.ReclaimFilter{}, "reaper")
	if err != nil {
		t.Fatalf("ReclaimExpiredLeases: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0].ID != "recown-1" || reclaimed[0].PreviousOwner != "jr-voice" {
		t.Fatalf("reclaimed = %+v, want [{recown-1 jr-voice}]", reclaimed)
	}
	got, err = te.store.GetIssue(ctx, "recown-1")
	if err != nil {
		t.Fatalf("GetIssue after reclaim: %v", err)
	}
	if got.Status != types.StatusOpen {
		t.Fatalf("status after reclaim = %q, want open", got.Status)
	}
	if got.Assignee != "jr-voice" {
		t.Fatalf("assignee after reclaim = %q, want jr-voice (canonical owner preserved)", got.Assignee)
	}

	if err := te.store.ClaimIssue(shortLease, "recown-1", "mallory"); !errors.Is(err, storage.ErrAlreadyClaimed) {
		t.Fatalf("ClaimIssue(mallory) err = %v, want ErrAlreadyClaimed", err)
	}
	if got, err = te.store.GetIssue(ctx, "recown-1"); err != nil {
		t.Fatalf("GetIssue after refused claim: %v", err)
	} else if got.Assignee != "jr-voice" {
		t.Fatalf("assignee after refused claim = %q, want jr-voice", got.Assignee)
	}

	if err := te.store.ClaimIssue(shortLease, "recown-1", "jr_voice"); err != nil {
		t.Fatalf("owner re-claim err = %v, want nil", err)
	}
	if got, err = te.store.GetIssue(ctx, "recown-1"); err != nil {
		t.Fatalf("GetIssue after owner re-claim: %v", err)
	} else if got.Assignee != "jr-voice" || got.Status != types.StatusInProgress {
		t.Fatalf("after owner re-claim assignee/status = %q/%q, want jr-voice/in_progress", got.Assignee, got.Status)
	}

	if err := te.store.UnclaimIssue(ctx, "recown-1", "jr_voice", false); err != nil {
		t.Fatalf("UnclaimIssue: %v", err)
	}
	if got, err = te.store.GetIssue(ctx, "recown-1"); err != nil {
		t.Fatalf("GetIssue after unclaim: %v", err)
	} else if got.Status != types.StatusOpen {
		t.Fatalf("status after unclaim = %q, want open", got.Status)
	} else if got.Assignee != "jr-voice" {
		t.Fatalf("assignee after unclaim = %q, want jr-voice (canonical owner preserved)", got.Assignee)
	}

	// Control: a one-off issue still releases its assignee on unclaim.
	oneOff := &types.Issue{
		ID:        "recown-2",
		Title:     "ordinary one-off task",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
	if err := te.store.CreateIssue(ctx, oneOff, "seeder"); err != nil {
		t.Fatalf("CreateIssue(one-off): %v", err)
	}
	if err := te.store.ClaimIssue(ctx, "recown-2", "bob"); err != nil {
		t.Fatalf("ClaimIssue(bob): %v", err)
	}
	if err := te.store.UnclaimIssue(ctx, "recown-2", "bob", false); err != nil {
		t.Fatalf("UnclaimIssue(bob): %v", err)
	}
	if got, err = te.store.GetIssue(ctx, "recown-2"); err != nil {
		t.Fatalf("GetIssue(one-off) after unclaim: %v", err)
	} else if got.Assignee != "" || got.Status != types.StatusOpen {
		t.Fatalf("one-off after unclaim assignee/status = %q/%q, want \"\"/open", got.Assignee, got.Status)
	}
}

// TestLegacyRecurrenceMetadataImportsInert pins the import half of the
// contract: a pre-contract issue whose metadata uses the recurrence keys
// without forming a valid schedule must import without failing the batch and
// claim leniently afterwards, exactly like it behaved before the contract.
func TestLegacyRecurrenceMetadataImportsInert(t *testing.T) {
	skipUnlessEmbeddedDolt(t)

	te := newTestEnv(t, "legimp")
	ctx := t.Context()

	legacy := &types.Issue{
		ID:        "legimp-1",
		Title:     "legacy weekly cadence",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Metadata:  json.RawMessage(`{"repeat":"weekly"}`),
	}
	if err := te.store.CreateIssuesWithFullOptions(ctx, []*types.Issue{legacy}, "import", storage.BatchCreateOptions{}); err != nil {
		t.Fatalf("CreateIssuesWithFullOptions with legacy recurrence metadata: %v", err)
	}

	got, err := te.store.GetIssue(ctx, "legimp-1")
	if err != nil {
		t.Fatalf("GetIssue after import: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(got.Metadata, &stored); err != nil {
		t.Fatalf("metadata after import is not a JSON object: %v (%s)", err, got.Metadata)
	}
	if len(stored) != 1 || stored["repeat"] != "weekly" {
		t.Fatalf("metadata after import = %s, want the legacy repeat key preserved", got.Metadata)
	}

	if err := te.store.ClaimIssue(ctx, "legimp-1", "alice"); err != nil {
		t.Fatalf("ClaimIssue(alice) on legacy-metadata issue: %v", err)
	}
	if got, err = te.store.GetIssue(ctx, "legimp-1"); err != nil {
		t.Fatalf("GetIssue after lenient claim: %v", err)
	} else if got.Assignee != "alice" || got.Status != types.StatusInProgress {
		t.Fatalf("after lenient claim assignee/status = %q/%q, want alice/in_progress", got.Assignee, got.Status)
	}
}
