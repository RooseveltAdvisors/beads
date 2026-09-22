package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_Journey verifies the full end-to-end notify journey ported from the
// prototype suite (test_beads_notify_e2e.sh):
//  1. request-comment: non-assignee comment enqueues notify row
//  2. attribution: actor resolved from BEADS_ACTOR or git config user.name
//  3. self-skip: self-comment (actor == assignee) never enqueues a row
//  4. follow-up vs steer selection: per-harness delivery matrix
//  5. hold-until-idle: unverified harness mid-turn stays queued without interrupting
//  6. blocked-pin escalation: approval dialog or dead pin marks parent escalation
func TestE2E_Journey(t *testing.T) {
	tempDir := t.TempDir()
	beadsDir := filepath.Join(tempDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outbox, err := Open(beadsDir)
	if err != nil {
		t.Fatal(err)
	}

	// -------------------------------------------------------------------------
	// 1. Attribution & Self-Skip
	// -------------------------------------------------------------------------
	t.Run("attribution and self-skip", func(t *testing.T) {
		t.Setenv("BEADS_ACTOR", "arcs-fm")
		actor := ResolveActor()
		if actor != "arcs-fm" {
			t.Fatalf("ResolveActor with BEADS_ACTOR=arcs-fm got %q, want 'arcs-fm'", actor)
		}

		// Self-comment: assignee equals actor -> must NOT enqueue
		seat, shouldPing := CommentAssigneeSeat("arcs-fm", actor)
		if shouldPing {
			t.Fatalf("CommentAssigneeSeat for self-comment should not ping, got seat=%q", seat)
		}

		// Case-insensitive self-comment
		_, shouldPingCase := CommentAssigneeSeat("ARCS-FM", "arcs-fm")
		if shouldPingCase {
			t.Fatal("CommentAssigneeSeat case-insensitive self-comment should not ping")
		}

		// Unassigned bead -> must NOT enqueue
		_, shouldPingUnassigned := CommentAssigneeSeat("unassigned", actor)
		if shouldPingUnassigned {
			t.Fatal("CommentAssigneeSeat for unassigned bead should not ping")
		}

		// Non-assignee comment -> must ping assignee
		targetSeat, shouldPingOther := CommentAssigneeSeat("arcs-fm", "wiseman")
		if !shouldPingOther || targetSeat != "arcs-fm" {
			t.Fatalf("CommentAssigneeSeat for coordinator comment should ping arcs-fm, got %q, %v", targetSeat, shouldPingOther)
		}
	})

	// -------------------------------------------------------------------------
	// 2. Request-Comment: Enqueueing and Title Formatting
	// -------------------------------------------------------------------------
	t.Run("request-comment enqueueing", func(t *testing.T) {
		title := CommentPingTitle("Refactor notify", "wiseman", "please check queue semantics")
		if !strings.Contains(title, "Refactor notify") || !strings.Contains(title, "wiseman") {
			t.Fatalf("CommentPingTitle unexpected format: %q", title)
		}

		rec, err := outbox.Enqueue("arcs-fm", "wiseman-kr2", KindComment, title)
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
		if rec.Seat != "arcs-fm" || rec.IssueID != "wiseman-kr2" || rec.Kind != KindComment {
			t.Fatalf("Enqueued record mismatch: %+v", rec)
		}

		pending, err := outbox.Pending("arcs-fm")
		if err != nil || len(pending) != 1 {
			t.Fatalf("Pending rows want 1, got %d (err: %v)", len(pending), err)
		}
		if pending[0].Title != title {
			t.Fatalf("Pending row title = %q, want %q", pending[0].Title, title)
		}
	})

	// -------------------------------------------------------------------------
	// 3. Follow-up vs Steer Selection
	// -------------------------------------------------------------------------
	t.Run("follow-up vs steer selection", func(t *testing.T) {
		pins := map[string]Pin{
			"arcs-fm": {Session: "firstmate", PaneID: "w1:pA"},
		}

		// Pi mid-turn (working): must deliver via follow-up (Option+Enter)
		livePiWorking := []Instance{
			{Session: "firstmate", PaneID: "w1:pA", Harness: "pi", Status: "working"},
		}
		decPi := DecideDelivery("arcs-fm", pins, livePiWorking)
		if !decPi.OK || decPi.Mode != ModeFollowUp || decPi.Escalate {
			t.Fatalf("pi working should deliver follow-up without steer: %+v", decPi)
		}
		callsPi := deliveryCalls(decPi.Instance, "test message", decPi.Mode)
		if len(callsPi) != 2 || !containsSeq(callsPi[1].Args, "send-keys", "w1:pA", "alt+enter") {
			t.Fatalf("pi follow-up calls should submit alt+enter: %v", callsPi)
		}

		// Cursor mid-turn (working): must deliver via tab queue
		liveCursorWorking := []Instance{
			{Session: "firstmate", PaneID: "w1:pA", Harness: "cursor", Status: "working"},
		}
		decCursor := DecideDelivery("arcs-fm", pins, liveCursorWorking)
		if !decCursor.OK || decCursor.Mode != ModeFollowUp || decCursor.Escalate {
			t.Fatalf("cursor working should deliver follow-up: %+v", decCursor)
		}
		callsCursor := deliveryCalls(decCursor.Instance, "test message", decCursor.Mode)
		if len(callsCursor) != 2 || !containsSeq(callsCursor[1].Args, "send-keys", "w1:pA", "tab") {
			t.Fatalf("cursor follow-up calls should submit tab: %v", callsCursor)
		}

		// Codex mid-turn (working): must deliver via tab queue
		liveCodexWorking := []Instance{
			{Session: "firstmate", PaneID: "w1:pA", Harness: "codex", Status: "working"},
		}
		decCodex := DecideDelivery("arcs-fm", pins, liveCodexWorking)
		if !decCodex.OK || decCodex.Mode != ModeFollowUp || decCodex.Escalate {
			t.Fatalf("codex working should deliver follow-up: %+v", decCodex)
		}
		callsCodex := deliveryCalls(decCodex.Instance, "test message", decCodex.Mode)
		if len(callsCodex) != 2 || !containsSeq(callsCodex[1].Args, "send-keys", "w1:pA", "tab") {
			t.Fatalf("codex follow-up calls should submit tab: %v", callsCodex)
		}

		// Claude mid-turn (working): hold-until-idle like agy
		liveClaudeWorking := []Instance{
			{Session: "firstmate", PaneID: "w1:pA", Harness: "claude", Status: "working"},
		}
		decClaude := DecideDelivery("arcs-fm", pins, liveClaudeWorking)
		if decClaude.OK || decClaude.Escalate || decClaude.Reason != HoldUntilIdle || decClaude.Classification != ExitDeferred {
			t.Fatalf("claude working should hold until idle: %+v", decClaude)
		}

		// Idle agent on any harness: delivered via Enter prompt (trivially non-interrupting)
		liveIdle := []Instance{
			{Session: "firstmate", PaneID: "w1:pA", Harness: "pi", Status: "idle"},
		}
		decIdle := DecideDelivery("arcs-fm", pins, liveIdle)
		if !decIdle.OK || decIdle.Mode != ModeSteer || decIdle.Escalate {
			t.Fatalf("idle target should deliver via steer/prompt: %+v", decIdle)
		}
	})

	// -------------------------------------------------------------------------
	// 4. Hold-Until-Idle
	// -------------------------------------------------------------------------
	t.Run("hold-until-idle delivery strategy", func(t *testing.T) {
		pins := map[string]Pin{
			"worker-agy": {Session: "worker", PaneID: "w2:p1"},
		}

		// Harnesses with unverified queue semantics while working: claude, claude-code, agy, gemini, kimi, omp, muse
		unverified := []string{"claude", "claude-code", "agy", "antigravity", "gemini", "kimi", "omp", "muse", "unknown-agent"}
		for _, h := range unverified {
			liveWorking := []Instance{
				{Session: "worker", PaneID: "w2:p1", Harness: h, Status: "working"},
			}
			dec := DecideDelivery("worker-agy", pins, liveWorking)
			if dec.OK {
				t.Fatalf("%s working must NOT deliver mid-turn without verified queue", h)
			}
			if dec.Escalate {
				t.Fatalf("%s working is holding until idle, should NOT escalate", h)
			}
			if dec.Reason != HoldUntilIdle {
				t.Fatalf("%s working reason = %q, want %q", h, dec.Reason, HoldUntilIdle)
			}
			if dec.Classification != ExitDeferred {
				t.Fatalf("%s working classification = %d, want ExitDeferred (%d)", h, dec.Classification, ExitDeferred)
			}
		}

		// When target transitions to idle, delivery succeeds
		liveNowIdle := []Instance{
			{Session: "worker", PaneID: "w2:p1", Harness: "agy", Status: "idle"},
		}
		decIdle := DecideDelivery("worker-agy", pins, liveNowIdle)
		if !decIdle.OK || decIdle.Mode != ModeSteer || decIdle.Classification != ExitDelivered {
			t.Fatalf("agy when idle must deliver: %+v", decIdle)
		}
	})

	// -------------------------------------------------------------------------
	// 5. Blocked Dialog & Dead Pin Escalation
	// -------------------------------------------------------------------------
	t.Run("blocked dialog and dead pin escalation", func(t *testing.T) {
		pins := map[string]Pin{
			"arcs-fm": {Session: "firstmate", PaneID: "w1:pA"},
		}

		// Blocked dialog: approval dialog open -> never type into it, escalate exit 8
		liveBlocked := []Instance{
			{Session: "firstmate", PaneID: "w1:pA", Harness: "pi", Status: "blocked"},
		}
		decBlocked := DecideDelivery("arcs-fm", pins, liveBlocked)
		if decBlocked.OK {
			t.Fatal("blocked target must NOT deliver")
		}
		if !decBlocked.Escalate {
			t.Fatal("blocked target MUST mark for parent escalation")
		}
		if decBlocked.Reason != HoldBlocked {
			t.Fatalf("blocked target reason = %q, want %q", decBlocked.Reason, HoldBlocked)
		}
		if decBlocked.Classification != ExitBlocked {
			t.Fatalf("blocked target classification = %d, want ExitBlocked (%d)", decBlocked.Classification, ExitBlocked)
		}

		// Record hold in outbox and verify
		if err := outbox.SetHold("arcs-fm", Hold{
			Reason:  decBlocked.Reason,
			Session: decBlocked.Pin.Session,
			PaneID:  decBlocked.Pin.PaneID,
			Pending: 1,
		}); err != nil {
			t.Fatalf("SetHold failed: %v", err)
		}
		holds, err := outbox.Holds()
		if err != nil || len(holds) != 1 {
			t.Fatalf("Holds want 1, got %+v", holds)
		}
		if holds["arcs-fm"].Reason != HoldBlocked {
			t.Fatalf("Hold reason = %q, want %q", holds["arcs-fm"].Reason, HoldBlocked)
		}

		// Dead pin: pinned target is missing/not live -> escalate exit 7
		liveEmpty := []Instance{
			{Session: "other-session", PaneID: "w9:p9", Harness: "pi", Status: "idle"},
		}
		decDead := DecideDelivery("arcs-fm", pins, liveEmpty)
		if decDead.OK || !decDead.Escalate || decDead.Reason != HoldDeadPin || decDead.Classification != ExitDeadPin {
			t.Fatalf("dead pin should escalate: %+v", decDead)
		}
	})
}
