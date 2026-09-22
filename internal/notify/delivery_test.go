package notify

import (
	"errors"
	"strings"
	"testing"
)

func TestModeForHarness(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		harness string
		mode    DeliveryMode
		key     string
	}{
		{"pi", "pi", ModeFollowUp, "alt+enter"},
		{"Pi case", "Pi", ModeFollowUp, "alt+enter"},
		{"pi-signed", "pi-signed", ModeFollowUp, "alt+enter"},
		{"cursor", "cursor", ModeFollowUp, "tab"},
		{"cursor-agent", "cursor-agent", ModeFollowUp, "tab"},
		{"codex", "codex", ModeFollowUp, "tab"},
		{"claude", "claude", ModeFollowUp, ""},
		{"claude-code", "claude-code", ModeFollowUp, ""},
		{"agy", "agy", ModeHoldUntilIdle, ""},
		{"antigravity", "antigravity", ModeHoldUntilIdle, ""},
		{"gemini", "gemini", ModeHoldUntilIdle, ""},
		{"kimi", "kimi", ModeHoldUntilIdle, ""},
		{"omp", "omp", ModeHoldUntilIdle, ""},
		{"muse", "muse", ModeHoldUntilIdle, ""},
		{"grok", "grok", ModeHoldUntilIdle, ""},
		{"empty", "", ModeHoldUntilIdle, ""},
		{"unknown", "unknown", ModeHoldUntilIdle, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ModeForHarness(tc.harness); got != tc.mode {
				t.Fatalf("ModeForHarness(%q) = %q, want %q", tc.harness, got, tc.mode)
			}
			if got := FollowUpSubmitKey(tc.harness); got != tc.key {
				t.Fatalf("FollowUpSubmitKey(%q) = %q, want %q", tc.harness, got, tc.key)
			}
		})
	}
}

func TestHarnessProfilesTable(t *testing.T) {
	t.Parallel()
	profiles := HarnessProfiles()
	if len(profiles) == 0 {
		t.Fatal("expected non-empty HarnessProfiles")
	}
	// Verify verified harnesses have Verified=true and non-empty notes
	verifiedCount := 0
	for _, p := range profiles {
		if p.Notes == "" {
			t.Errorf("profile %s has empty notes", p.Harness)
		}
		if p.Verified {
			verifiedCount++
			if p.HoldUntilIdle {
				t.Errorf("verified harness %s should not default to hold-until-idle", p.Harness)
			}
		} else {
			if !p.HoldUntilIdle {
				t.Errorf("unverified harness %s must default to hold-until-idle", p.Harness)
			}
		}
	}
	if verifiedCount < 5 {
		t.Errorf("expected at least 5 verified harness rows (pi, pi-signed, cursor, cursor-agent, codex, claude, claude-code), got %d", verifiedCount)
	}
}

func TestPromptSelectsFollowUpFromPiHarness(t *testing.T) {
	t.Parallel()
	prompt := DefaultPrompt(Record{Kind: KindDue, Seat: "wiseman", IssueID: "bd-1", Title: "Do thing"})
	var got [][]string
	h := HerdrDiscoverer{
		runFn: func(args ...string) ([]byte, error) {
			got = append(got, append([]string(nil), args...))
			return nil, nil
		},
	}
	if err := h.Prompt(Instance{Session: "wiseman", PaneID: "w1:p1", Harness: "pi"}, prompt); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if len(got) != 2 || !containsSeq(got[1], "send-keys", "w1:p1", "alt+enter") {
		t.Fatalf("Prompt on pi should follow-up with Option+Enter, got %v", got)
	}
}

func TestDeliverPiFollowUpUsesAltEnter(t *testing.T) {
	t.Parallel()
	rec := Record{Kind: KindDue, Seat: "wiseman", IssueID: "bd-1", Title: "Do thing"}
	prompt := DefaultPrompt(rec)
	inst := Instance{Session: "wiseman", PaneID: "w1:p1", Harness: "pi"}

	got := captureDeliver(t, inst, prompt, ModeFollowUp)
	if len(got) != 2 {
		t.Fatalf("pi follow-up calls = %d, want 2 (send-text + send-keys): %v", len(got), got)
	}
	if !containsSeq(got[0], "--session", "wiseman", "pane", "send-text", "w1:p1") {
		t.Fatalf("first call should paste into the pane, got %v", got[0])
	}
	if promptText(deliveryCall{Args: got[0]}) != prompt {
		t.Fatalf("pasted body drifted from notify prompt")
	}
	if !containsSeq(got[1], "--session", "wiseman", "pane", "send-keys", "w1:p1", "alt+enter") {
		t.Fatalf("second call should submit as Option+Enter follow-up, got %v", got[1])
	}
	if containsSeq(got[0], "agent", "prompt") || containsSeq(got[1], "agent", "prompt") {
		t.Fatal("pi follow-up must not use agent prompt (Enter/steer)")
	}
}

func TestDeliverPiFollowUpCtrlJFallback(t *testing.T) {
	t.Parallel()
	prompt := "hello"
	inst := Instance{Session: "wiseman", PaneID: "w1:p1", Harness: "pi"}
	var calls [][]string
	h := HerdrDiscoverer{
		runFn: func(args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			if containsSeq(args, "send-keys", "w1:p1", "alt+enter") {
				return nil, errors.New("alt+enter unsupported in terminal")
			}
			return nil, nil
		},
	}
	if err := h.Deliver(inst, prompt, ModeFollowUp); err != nil {
		t.Fatalf("Deliver with fallback failed: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls (paste, alt+enter, ctrl+j), got %d: %v", len(calls), calls)
	}
	if !containsSeq(calls[2], "send-keys", "w1:p1", "ctrl+j") {
		t.Fatalf("expected ctrl+j fallback call, got %v", calls[2])
	}
}

func TestDeliverSteerUsesAgentPromptWhenIdle(t *testing.T) {
	t.Parallel()
	rec := Record{Kind: KindDue, Seat: "wiseman", IssueID: "bd-1", Title: "Do thing"}
	prompt := DefaultPrompt(rec)
	for _, harness := range []string{"claude", "omp", "grok", "agy", "gemini", ""} {
		inst := Instance{Session: "wiseman", PaneID: "w1:p2", Harness: harness, Status: "idle"}
		got := captureDeliver(t, inst, prompt, ModeSteer)
		if len(got) != 1 {
			t.Fatalf("%s steer calls = %d, want 1: %v", harness, len(got), got)
		}
		if !containsSeq(got[0], "agent", "prompt", "w1:p2") {
			t.Fatalf("%s should prompt via agent prompt, got %v", harness, got[0])
		}
		if promptText(deliveryCall{Args: got[0]}) != prompt {
			t.Fatalf("%s body drifted from notify prompt", harness)
		}
	}
}

func TestDeliverContentIdenticalAcrossModes(t *testing.T) {
	t.Parallel()
	rec := Record{Kind: KindComment, Seat: "wiseman", IssueID: "bd-9", Title: "hello"}
	prompt := DefaultPrompt(rec)
	pi := Instance{Session: "s", PaneID: "p1", Harness: "pi"}
	claude := Instance{Session: "s", PaneID: "p1", Harness: "claude"}

	follow := deliveryCalls(pi, prompt, ModeFollowUp)
	steer := deliveryCalls(claude, prompt, ModeSteer)
	if len(follow) == 0 || len(steer) == 0 {
		t.Fatal("expected delivery calls in both modes")
	}
	followText := promptText(follow[0])
	steerText := promptText(steer[0])
	if followText != prompt || steerText != prompt {
		t.Fatalf("payload must be DefaultPrompt in both modes\nfollow=%q\nsteer=%q\nprompt=%q", followText, steerText, prompt)
	}
	if followText != steerText {
		t.Fatalf("follow-up and steer payloads differ:\nfollow=%q\nsteer=%q", followText, steerText)
	}
}

func TestFollowUpSubmitKeysOnSupportedHarnesses(t *testing.T) {
	t.Parallel()
	prompt := DefaultPrompt(Record{Kind: KindDue, Seat: "wiseman", IssueID: "bd-1", Title: "Do thing"})
	cases := []struct {
		harness string
		key     string
	}{
		{"pi", "alt+enter"},
		{"cursor", "tab"},
		{"codex", "tab"},
	}
	for _, tc := range cases {
		t.Run(tc.harness, func(t *testing.T) {
			t.Parallel()
			inst := Instance{Session: "s", PaneID: "p1", Harness: tc.harness}
			calls := deliveryCalls(inst, prompt, ModeFollowUp)
			if len(calls) != 2 {
				t.Fatalf("calls = %d, want 2: %#v", len(calls), calls)
			}
			if promptText(calls[0]) != prompt {
				t.Fatalf("follow-up body drifted from notify prompt")
			}
			if !containsSeq(calls[1].Args, "pane", "send-keys", "p1", tc.key) {
				t.Fatalf("want send-keys %s, got %v", tc.key, calls[1].Args)
			}
		})
	}
}

func TestFollowUpOnClaudeUsesNativeQueueEnter(t *testing.T) {
	t.Parallel()
	inst := Instance{Session: "s", PaneID: "p9", Harness: "claude"}
	calls := deliveryCalls(inst, "hello", ModeFollowUp)
	if len(calls) != 1 || !containsSeq(calls[0].Args, "agent", "prompt") {
		t.Fatalf("claude native queue uses Enter (agent prompt), got %#v", calls)
	}
}

func TestDecideDeliveryMatrix(t *testing.T) {
	t.Parallel()
	pins := map[string]Pin{"wiseman": {Session: "wiseman", PaneID: "w1:p1"}}

	// 1. Pi working -> follow-up
	live := []Instance{{Session: "wiseman", PaneID: "w1:p1", Harness: "pi", Status: "working"}}
	got := DecideDelivery("wiseman", pins, live)
	if !got.OK || got.Mode != ModeFollowUp || got.Escalate {
		t.Fatalf("pi working seat: %+v", got)
	}

	// 2. Claude working -> follow-up (native busy queue)
	live[0].Harness = "claude"
	live[0].Status = "working"
	got = DecideDelivery("wiseman", pins, live)
	if !got.OK || got.Mode != ModeFollowUp || got.Escalate {
		t.Fatalf("claude working seat: %+v", got)
	}

	// 3. Claude idle -> steer (prompt Enter)
	live[0].Status = "idle"
	got = DecideDelivery("wiseman", pins, live)
	if !got.OK || got.Mode != ModeSteer || got.Escalate {
		t.Fatalf("claude idle seat: %+v", got)
	}

	// 4. Agy working -> hold-until-idle (OK=false, Escalate=false, Reason=HoldUntilIdle, Classification=ExitDeferred)
	live[0].Harness = "agy"
	live[0].Status = "working"
	got = DecideDelivery("wiseman", pins, live)
	if got.OK || got.Escalate || got.Reason != HoldUntilIdle || got.Classification != ExitDeferred {
		t.Fatalf("agy working seat: %+v", got)
	}

	// 5. Agy idle -> prompt Enter (OK=true, Mode=ModeSteer, Classification=ExitDelivered)
	live[0].Status = "idle"
	got = DecideDelivery("wiseman", pins, live)
	if !got.OK || got.Escalate || got.Mode != ModeSteer || got.Classification != ExitDelivered {
		t.Fatalf("agy idle seat: %+v", got)
	}

	// 6. Blocked agent -> never type into approval dialog (OK=false, Escalate=true, Reason=HoldBlocked, Classification=ExitBlocked)
	live[0].Status = "blocked"
	got = DecideDelivery("wiseman", pins, live)
	if got.OK || !got.Escalate || got.Reason != HoldBlocked || got.Classification != ExitBlocked {
		t.Fatalf("blocked seat: %+v", got)
	}

	// 7. Dead pin -> OK=false, Escalate=true, Reason=HoldDeadPin, Classification=ExitDeadPin
	got = DecideDelivery("wiseman", pins, []Instance{})
	if got.OK || !got.Escalate || got.Reason != HoldDeadPin || got.Classification != ExitDeadPin {
		t.Fatalf("dead pin seat: %+v", got)
	}
}

func captureDeliver(t *testing.T, inst Instance, text string, mode DeliveryMode) [][]string {
	t.Helper()
	var got [][]string
	h := HerdrDiscoverer{
		runFn: func(args ...string) ([]byte, error) {
			cp := append([]string(nil), args...)
			got = append(got, cp)
			return nil, nil
		},
	}
	if err := h.Deliver(inst, text, mode); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	return got
}

func promptText(call deliveryCall) string {
	if len(call.Args) == 0 {
		return ""
	}
	text := call.Args[len(call.Args)-1]
	if strings.HasPrefix(text, bracketedPasteStart) && strings.HasSuffix(text, bracketedPasteEnd) {
		return strings.TrimSuffix(strings.TrimPrefix(text, bracketedPasteStart), bracketedPasteEnd)
	}
	return text
}

func containsSeq(args []string, seq ...string) bool {
	if len(seq) == 0 || len(seq) > len(args) {
		return false
	}
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j := range seq {
			if args[i+j] != seq[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
