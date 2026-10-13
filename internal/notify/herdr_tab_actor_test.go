package notify

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeHerdr installs a stub herdr that prints fixed tab-list JSON and points
// HERDR_BIN at it, so the lookup is testable without a live session.
func fakeHerdr(t *testing.T, script string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "herdr")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake herdr: %v", err)
	}
	t.Setenv("HERDR_BIN", p)
}

const tabListJSON = `#!/usr/bin/env bash
cat <<'JSON'
{"result":{"tabs":[{"tab_id":"w1:t9","label":"portal AMD biller"},{"tab_id":"w1:t8","label":""}]}}
JSON
`

func TestHerdrTabActor(t *testing.T) {
	t.Setenv("HERDR_SESSION", "wiseman")
	t.Setenv("HERDR_TAB_ID", "w1:t9")
	fakeHerdr(t, tabListJSON)
	if got := HerdrTabActor(); got != "portal AMD biller" {
		t.Fatalf("HerdrTabActor() = %q, want %q", got, "portal AMD biller")
	}
}

func TestHerdrTabActorEmptyLabelOrUnknownTab(t *testing.T) {
	t.Setenv("HERDR_SESSION", "wiseman")
	fakeHerdr(t, tabListJSON)
	t.Setenv("HERDR_TAB_ID", "w1:t8") // tab exists but has no label
	if got := HerdrTabActor(); got != "" {
		t.Fatalf("unlabeled tab must yield empty actor, got %q", got)
	}
	t.Setenv("HERDR_TAB_ID", "w1:zz")
	if got := HerdrTabActor(); got != "" {
		t.Fatalf("unknown tab must yield empty actor, got %q", got)
	}
}

func TestHerdrTabActorOutsideHerdOrHerdrFailure(t *testing.T) {
	t.Setenv("HERDR_SESSION", "")
	t.Setenv("HERDR_TAB_ID", "")
	if got := HerdrTabActor(); got != "" {
		t.Fatalf("outside herdr (no env) must yield empty actor, got %q", got)
	}
	t.Setenv("HERDR_SESSION", "wiseman")
	t.Setenv("HERDR_TAB_ID", "w1:t9")
	fakeHerdr(t, "#!/usr/bin/env bash\nexit 1\n")
	if got := HerdrTabActor(); got != "" {
		t.Fatalf("a failing herdr must degrade to empty actor, got %q", got)
	}
}

func TestResolveActorExplicitBeatsTabLabel(t *testing.T) {
	t.Setenv("HERDR_SESSION", "wiseman")
	t.Setenv("HERDR_TAB_ID", "w1:t9")
	fakeHerdr(t, tabListJSON)

	t.Setenv("BEADS_ACTOR", "explicit-seat")
	if got := ResolveActor(); got != "explicit-seat" {
		t.Fatalf("BEADS_ACTOR must win over the tab label, got %q", got)
	}
	t.Setenv("BEADS_ACTOR", "")
	if got := ResolveActor(); got != "portal AMD biller" {
		t.Fatalf("inside a named tab the label must win over git identity, got %q", got)
	}
}
