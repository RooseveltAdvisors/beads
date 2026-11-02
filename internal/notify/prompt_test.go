package notify

import (
	"strings"
	"testing"
)

// bannedSteering is the copy that made agents over-apply notifications:
// command bodies that ordered work, stamping, captain isolation, and idling
// (captain order 2026-11-01: reminders must never steer).
var bannedSteering = []string{
	"Go idle",
	"Do the work yourself",
	"Stamp progress",
	"stamp progress",
	"Do not ask the captain",
	"Do not only reply in chat",
	"Read it, act,",
}

// TestDefaultPrompt_RemindsWithoutSteering is the core contract: every kind
// names what happened, points at the thread, disclaims priority changes, and
// carries none of the old command copy.
func TestDefaultPrompt_RemindsWithoutSteering(t *testing.T) {
	for _, kind := range []Kind{KindDue, KindComment, KindStaleClaim, KindProgress, KindEscalate, KindDefer, KindManual} {
		rec := Record{Kind: kind, Seat: "wiseman", IssueID: "fm-abc", Title: "Do thing"}
		p := DefaultPrompt(rec)
		for _, want := range []string{
			"BEADS NOTIFY (" + string(kind) + ")",
			"fm-abc",
			"bd comments fm-abc",
			"does not\nchange your priorities",
			"never a reason to\nstop or go idle",
		} {
			if !strings.Contains(p, want) {
				t.Errorf("%s prompt missing %q", kind, want)
			}
		}
		for _, ban := range bannedSteering {
			if strings.Contains(p, ban) {
				t.Errorf("%s prompt carries banned steering copy %q", kind, ban)
			}
		}
	}
}

func TestDefaultPrompt_KindLines(t *testing.T) {
	cases := map[Kind]string{
		KindComment:    "New comment on a bead your seat owns.",
		KindStaleClaim: "still open under your seat",
		KindProgress:   "wants a delta comment",
		KindEscalate:   "escalated after repeated misses",
		KindDue:        "due date arrived",
	}
	for kind, want := range cases {
		if p := DefaultPrompt(Record{Kind: kind, IssueID: "x"}); !strings.Contains(p, want) {
			t.Errorf("%s line missing %q in:\n%s", kind, want, p)
		}
	}
}

// TestDefaultPrompt_PriorityTag pins the triage tag: a record carrying a
// priority renders [P0]-[P4] beside the bead id; legacy rows without one
// render clean (captain order 2026-11-02).
func TestDefaultPrompt_PriorityTag(t *testing.T) {
	p2 := 2
	p0 := 0
	withP := DefaultPrompt(Record{Kind: KindDue, Seat: "s", IssueID: "fm-1", Title: "T", Priority: &p2})
	if !strings.Contains(withP, "Bead fm-1 [P2]") {
		t.Errorf("priority tag missing:\n%s", withP)
	}
	withP0 := DefaultPrompt(Record{Kind: KindDue, Seat: "s", IssueID: "fm-1", Title: "T", Priority: &p0})
	if !strings.Contains(withP0, "Bead fm-1 [P0]") {
		t.Errorf("P0 tag missing:\n%s", withP0)
	}
	legacy := DefaultPrompt(Record{Kind: KindDue, Seat: "s", IssueID: "fm-1", Title: "T"})
	if !strings.Contains(legacy, "Bead fm-1 fired") || strings.Contains(legacy, "[P") {
		t.Errorf("legacy row must render without a tag:\n%s", legacy)
	}
}
