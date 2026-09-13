package notify

import "testing"

func TestResolveSeatPrefersSessionName(t *testing.T) {
	instances := []Instance{
		{Session: "firstmate", PaneID: "w1:p9", Harness: "pi", Status: "idle", Title: "π - firstmate", Focused: true},
		{Session: "wiseman", PaneID: "w1:p1", Harness: "pi", Status: "working", Title: "π - wiseman", Focused: true},
		{Session: "firstmate", PaneID: "w6:p2", Harness: "claude", Status: "idle", Title: "some advisory", Cwd: "/tmp/x"},
	}
	got, ok := ResolveSeat("wiseman", instances)
	if !ok || got.PaneID != "w1:p1" || got.Session != "wiseman" {
		t.Fatalf("wiseman → %+v ok=%v", got, ok)
	}
	got, ok = ResolveSeat("firstmate", instances)
	if !ok || got.PaneID != "w1:p9" {
		t.Fatalf("firstmate primary → %+v ok=%v", got, ok)
	}
}

func TestResolveSeatTitleAndCwdWithoutSession(t *testing.T) {
	instances := []Instance{
		{Session: "firstmate", PaneID: "w0:p2", Harness: "claude", Status: "idle", Title: "Arcs Macro ops", Cwd: "/work/portal-ops"},
		{Session: "adhoc", PaneID: "w1:p3", Harness: "codex", Status: "idle", Title: "π - portal-ops", Focused: true},
	}
	got, ok := ResolveSeat("portal-ops", instances)
	if !ok {
		t.Fatal("expected match")
	}
	// title token on adhoc scores 40+10+5; cwd on firstmate scores 20+5
	// title+focused should win
	if got.PaneID != "w1:p3" {
		t.Fatalf("want title match pane, got %+v", got)
	}
}

func TestResolveSeatNoMatch(t *testing.T) {
	_, ok := ResolveSeat("nobody", []Instance{
		{Session: "firstmate", PaneID: "w1:p1", Title: "π - firstmate"},
	})
	if ok {
		t.Fatal("expected no match")
	}
	_, ok = ResolveSeat("unassigned", []Instance{
		{Session: "unassigned", PaneID: "w1:p1"},
	})
	if ok {
		t.Fatal("unassigned seat must not resolve")
	}
}

func TestTitleHasSeatToken(t *testing.T) {
	if !titleHasSeatToken("π - wiseman", "wiseman") {
		t.Fatal("expected token match")
	}
	if titleHasSeatToken("π - firstmate", "mate") {
		t.Fatal("substring should not match without token boundary")
	}
}
