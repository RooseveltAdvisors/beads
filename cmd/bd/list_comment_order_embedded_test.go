//go:build cgo

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestEmbeddedListCommentAdvancesUpdatedSort pins the acceptance split from
// /tmp/beads-comment-order-qa.1vTXlL/comparison.json: create A, create B,
// comment on A, then `bd list --sort updated` must list A first (published
// 016b233e33bc behavior), while status and assignee stay put and the show
// revision token (RowVersion) must not change across the comment (upstream
// CommenterAdd invariant).
func TestEmbeddedListCommentAdvancesUpdatedSort(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "lco")

	older := bdCreate(t, bd, dir, "older commented issue", "--type", "task")
	newer := bdCreate(t, bd, dir, "newer unmodified issue", "--type", "task")

	beforeShow, err := bdRunWithFlockRetry(t, bd, dir, "show", older.ID, "--json")
	if err != nil {
		t.Fatalf("bd show before comment: %v\n%s", err, beforeShow)
	}
	before := parseShowJSONObject(t, beforeShow)
	beforeRev, _ := before["revision"].(string)
	beforeStatus, _ := before["status"].(string)
	beforeAssignee, _ := before["assignee"].(string)
	if beforeRev == "" || beforeRev == "0" {
		t.Fatalf("pre-comment revision = %q; want a live token", beforeRev)
	}

	if out, err := bdRunWithFlockRetry(t, bd, dir, "comment", older.ID, "activity bump"); err != nil {
		t.Fatalf("bd comment: %v\n%s", err, out)
	}

	afterShow, err := bdRunWithFlockRetry(t, bd, dir, "show", older.ID, "--json")
	if err != nil {
		t.Fatalf("bd show after comment: %v\n%s", err, afterShow)
	}
	after := parseShowJSONObject(t, afterShow)
	afterRev, _ := after["revision"].(string)
	afterStatus, _ := after["status"].(string)
	afterAssignee, _ := after["assignee"].(string)

	if afterRev != beforeRev {
		t.Errorf("revision moved %q -> %q across a comment; RowVersion must hold", beforeRev, afterRev)
	}
	if afterStatus != beforeStatus {
		t.Errorf("status moved %q -> %q across a comment", beforeStatus, afterStatus)
	}
	if afterAssignee != beforeAssignee {
		t.Errorf("assignee moved %q -> %q across a comment", beforeAssignee, afterAssignee)
	}

	listOut, err := bdRunWithFlockRetry(t, bd, dir, "list", "--json", "--sort", "updated", "--status", "all")
	if err != nil {
		t.Fatalf("bd list --sort updated: %v\n%s", err, listOut)
	}
	s := string(listOut)
	start := strings.Index(s, "[")
	if start < 0 {
		t.Fatalf("no JSON array in list output:\n%s", s)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(s[start:]), &rows); err != nil {
		t.Fatalf("parse list JSON: %v\n%s", err, s[start:])
	}
	if len(rows) < 2 {
		t.Fatalf("list returned %d rows, want at least the 2 seeded", len(rows))
	}
	firstID, _ := rows[0]["id"].(string)
	if firstID != older.ID {
		t.Fatalf("list --sort updated first id = %q, want commented issue %q (second was %q); comparison.json published first_after=older",
			firstID, older.ID, newer.ID)
	}

	commentsOut, err := bdRunWithFlockRetry(t, bd, dir, "comments", older.ID, "--json")
	if err != nil {
		t.Fatalf("bd comments: %v\n%s", err, commentsOut)
	}
	cs := string(commentsOut)
	cstart := strings.Index(cs, "[")
	if cstart < 0 {
		t.Fatalf("no JSON array in comments output:\n%s", cs)
	}
	var comments []map[string]any
	if err := json.Unmarshal([]byte(cs[cstart:]), &comments); err != nil {
		t.Fatalf("parse comments JSON: %v\n%s", err, cs[cstart:])
	}
	if len(comments) != 1 {
		t.Fatalf("comment_count = %d, want 1 (comparison.json)", len(comments))
	}
}

func parseShowJSONObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	s := string(raw)
	start := strings.Index(s, "{")
	if start < 0 {
		t.Fatalf("no JSON object in show output:\n%s", s)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(s[start:]), &obj); err != nil {
		t.Fatalf("parse show JSON: %v\n%s", err, s[start:])
	}
	return obj
}
