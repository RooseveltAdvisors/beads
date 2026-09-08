//go:build cgo

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestProxiedServerRecurringCreate pins the proxied-server create route: the
// recurrence flags ride gatherCreateInput into the created issue, invalid
// schedules are refused without creating a one-off stand-in, and the
// markdown/graph routes reject the flags instead of silently ignoring them.
func TestProxiedServerRecurringCreate(t *testing.T) {
	requireSharedProxiedServer(t)
	t.Parallel()

	bd := buildEmbeddedBD(t)

	t.Run("valid_recurring_create_persists_contract", func(t *testing.T) {
		t.Parallel()
		p := newSharedProxiedProject(t, bd, "prc")
		issue := bdProxiedCreate(t, bd, p.dir,
			"Publish daily jonroosevelt.com blog post",
			"--assignee", "jr-voice",
			"--repeat", "0 9 * * *",
			"--recurrence-start", "2026-09-08",
			"--recurrence-tz", "America/New_York",
		)
		db := openProxiedDB(t, p)
		var metadata string
		if err := db.QueryRowContext(context.Background(), "SELECT COALESCE(metadata, '') FROM issues WHERE id = ?", issue.ID).Scan(&metadata); err != nil {
			t.Fatalf("query persisted metadata: %v", err)
		}
		recurrence, err := types.ParseRecurrence(json.RawMessage(metadata), "jr-voice")
		if err != nil {
			t.Fatalf("persisted metadata does not parse recurring: %v (%s)", err, metadata)
		}
		if recurrence.Schedule != "0 9 * * *" || recurrence.Start != "2026-09-08" || recurrence.Timezone != "America/New_York" {
			t.Fatalf("persisted recurrence = %+v", recurrence)
		}
	})

	t.Run("invalid_repeat_fails_validation_without_creating", func(t *testing.T) {
		t.Parallel()
		p := newSharedProxiedProject(t, bd, "prx")
		out := bdProxiedCreateFail(t, bd, p.dir,
			"Broken schedule",
			"--assignee", "jr-voice",
			"--repeat", "61 9 * * *",
			"--recurrence-start", "2026-09-08",
			"--recurrence-tz", "UTC",
		)
		if !strings.Contains(out, "invalid recurrence") {
			t.Fatalf("output missing validation refusal:\n%s", out)
		}
		db := openProxiedDB(t, p)
		var n int
		if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM issues WHERE title = ?", "Broken schedule").Scan(&n); err != nil {
			t.Fatalf("count issues: %v", err)
		}
		if n != 0 {
			t.Fatalf("invalid recurring create left %d issue row(s) behind", n)
		}
	})

	t.Run("markdown_and_graph_routes_reject_recurrence_flags", func(t *testing.T) {
		t.Parallel()
		p := newSharedProxiedProject(t, bd, "prj")
		markdownFile := filepath.Join(t.TempDir(), "issues.md")
		if err := os.WriteFile(markdownFile, []byte("---\ntitle: From markdown\n---\nBody\n"), 0o644); err != nil {
			t.Fatalf("write markdown: %v", err)
		}
		out := bdProxiedCreateFail(t, bd, p.dir, "--file", markdownFile, "--repeat", "daily")
		if !strings.Contains(out, "--repeat") {
			t.Fatalf("markdown create output missing --repeat refusal:\n%s", out)
		}
		graphFile := filepath.Join(t.TempDir(), "plan.json")
		if err := os.WriteFile(graphFile, []byte(`{"nodes":[]}`), 0o644); err != nil {
			t.Fatalf("write graph: %v", err)
		}
		out = bdProxiedCreateFail(t, bd, p.dir, "--graph", graphFile, "--recurrence-tz", "UTC")
		if !strings.Contains(out, "--recurrence-tz") {
			t.Fatalf("graph create output missing --recurrence-tz refusal:\n%s", out)
		}
	})
}
