//go:build cgo && integration

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommentAuthorFromHerdrTabLabel locks the comment attribution rule end to
// end through the real command path: inside a captain-named herdr tab the
// stored comment author is the tab label, not git user.name (wiseman-1fie:
// a real comment from the "portal AMD biller" tab was filed as the clone's
// git identity, and because that equals the assignee the ping was suppressed).
func TestCommentAuthorFromHerdrTabLabel(t *testing.T) {
	dir := setupCLITestDB(t)

	stub := filepath.Join(t.TempDir(), "herdr")
	stubScript := "#!/usr/bin/env bash\ncat <<'JSON'\n{\"result\":{\"tabs\":[{\"tab_id\":\"w1:t9\",\"label\":\"portal-AMD-biller\"}]}}\nJSON\n"
	if err := os.WriteFile(stub, []byte(stubScript), 0o755); err != nil {
		t.Fatalf("write fake herdr: %v", err)
	}
	t.Setenv("HERDR_BIN", stub)
	t.Setenv("HERDR_SESSION", "wiseman")
	t.Setenv("HERDR_TAB_ID", "w1:t9")
	// Explicit overrides must be empty for the tab label to be the default.
	t.Setenv("BEADS_ACTOR", "")
	t.Setenv("BD_ACTOR", "")

	createOut := runBDInProcess(t, dir, "create", "tab label attribution", "-p", "1", "--json")
	idx := strings.Index(createOut, "{")
	if idx < 0 {
		t.Fatalf("no JSON from create: %s", createOut)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createOut[idx:]), &created); err != nil || created.ID == "" {
		t.Fatalf("cannot parse create output: %v\n%s", err, createOut)
	}

	runBDInProcess(t, dir, "comment", created.ID, "attribution probe")

	out := runBDInProcess(t, dir, "comments", created.ID, "--json")
	var rows []struct {
		Author string `json:"author"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) == 0 {
		t.Fatalf("cannot parse comments output: %v\n%s", err, out)
	}
	if rows[0].Author != "portal-AMD-biller" {
		t.Fatalf("comment author = %q, want the herdr tab label %q (env HERDR_TAB_ID=%q)",
			rows[0].Author, "portal-AMD-biller", os.Getenv("HERDR_TAB_ID"))
	}
}
