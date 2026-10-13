package notify

import (
	"strings"
	"testing"
)

func TestDefaultPrompt_TeachesClose(t *testing.T) {
	rec := Record{Kind: KindDue, Seat: "wiseman", IssueID: "fm-abc", Title: "Do thing"}
	p := DefaultPrompt(rec)
	for _, want := range []string{"BEADS NOTIFY (due)", "fm-abc", "bd close fm-abc", "comment.progress_required"} {
		if !strings.Contains(p, want) {
			t.Fatalf("due prompt missing %q:\n%s", want, p)
		}
	}
}

func TestDefaultPrompt_StaleClaimPlaybook(t *testing.T) {
	rec := Record{Kind: KindStaleClaim, Seat: "firstmate", IssueID: "fm-stale", Title: "zombie"}
	p := DefaultPrompt(rec)
	for _, want := range []string{
		"stale-claim",
		"NOT done in beads",
		"bd comment fm-stale",
		"bd close fm-stale --reason",
		"--force-no-comment",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("stale-claim prompt missing %q:\n%s", want, p)
		}
	}
}

func TestDefaultPrompt_ProgressPlaybook(t *testing.T) {
	rec := Record{Kind: KindProgress, Seat: "x", IssueID: "fm-p", Title: "need comment"}
	p := DefaultPrompt(rec)
	if !strings.Contains(p, "Progress trail missing") || !strings.Contains(p, "bd close fm-p --reason") {
		t.Fatalf("progress prompt weak:\n%s", p)
	}
}
