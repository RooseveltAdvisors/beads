package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/notify"
)

func setupNotifyBeadsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DIR", dir)
	return dir
}

func TestPingCommentAssignee_EnqueuesKindComment(t *testing.T) {
	t.Setenv("BEADS_TEST_MODE", "1")
	dir := setupNotifyBeadsDir(t)

	pingCommentAssignee("wiseman", "fm-c", "Do thing", "firstmate", "please look")

	o, err := notify.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := o.Pending("wiseman")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	if pending[0].Kind != notify.KindComment || pending[0].IssueID != "fm-c" {
		t.Fatalf("row = %+v", pending[0])
	}
	holds, err := o.Holds()
	if err != nil {
		t.Fatal(err)
	}
	if len(holds) != 0 {
		t.Fatalf("test mode must skip drain/hold, got %v", holds)
	}
}

func TestPingCommentAssignee_SkipsSelfAndUnassigned(t *testing.T) {
	t.Setenv("BEADS_TEST_MODE", "1")
	dir := setupNotifyBeadsDir(t)

	pingCommentAssignee("", "fm-c", "T", "firstmate", "hello")
	pingCommentAssignee("unassigned", "fm-c", "T", "firstmate", "hello")
	pingCommentAssignee("wiseman", "fm-c", "T", "wiseman", "hello")

	o, err := notify.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range []string{"wiseman", "unassigned"} {
		pending, err := o.Pending(seat)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) != 0 {
			t.Fatalf("seat %s pending = %+v, want none", seat, pending)
		}
	}
}

func TestTryDrainCommentPing_NoPinHoldsWithoutHerdr(t *testing.T) {
	t.Setenv("BEADS_TEST_MODE", "")
	dir := setupNotifyBeadsDir(t)

	o, err := notify.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Enqueue("wiseman", "fm-c", notify.KindComment, "hello"); err != nil {
		t.Fatal(err)
	}
	tryDrainCommentPing(o, "wiseman")

	holds, err := o.Holds()
	if err != nil {
		t.Fatal(err)
	}
	h, ok := holds["wiseman"]
	if !ok || h.Reason != notify.HoldNoPin {
		t.Fatalf("want no-pin hold, got %v", holds)
	}
	pending, err := o.Pending("wiseman")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("unpinned drain must leave the row queued, got %d", len(pending))
	}
}
