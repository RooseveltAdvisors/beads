//go:build cgo

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/dedup"
	"github.com/steveyegge/beads/internal/types"
)

// clearDedupCheckEnv drops the package-wide BD_CREATE_DEDUP_CHECK=false opt-out
// (set in TestMain) so these tests see the real default and config handling.
func clearDedupCheckEnv(t *testing.T) {
	t.Helper()
	if v, ok := os.LookupEnv("BD_CREATE_DEDUP_CHECK"); ok {
		_ = os.Unsetenv("BD_CREATE_DEDUP_CHECK")
		t.Cleanup(func() { _ = os.Setenv("BD_CREATE_DEDUP_CHECK", v) })
	}
}

// TestCreateDedup_FormatDuplicateBlockError verifies Firstmate ruling 5:
// The error text must name the conflict first, then the two bypass flags,
// then the config key - in that order, one screen.
func TestCreateDedup_FormatDuplicateBlockError(t *testing.T) {
	conflictIssue := &types.Issue{
		ID:       "bd-1234",
		Title:    "Existing issue title",
		Status:   types.StatusOpen,
		Priority: 1,
		Assignee: "alice",
	}
	conflict := &dedup.ConflictResult{
		ConflictIssue: conflictIssue,
		Similarity:    0.85,
		Reason:        "85% similarity",
	}

	errText := formatDuplicateBlockError(conflict)

	// Check sections exist
	if !strings.Contains(errText, "blocked: potential duplicate of active issue bd-1234 (85% similar)") {
		t.Fatalf("missing or incorrect conflict header: %s", errText)
	}
	if !strings.Contains(errText, "ID:       bd-1234") ||
		!strings.Contains(errText, "Title:    Existing issue title") ||
		!strings.Contains(errText, "Status:   open") ||
		!strings.Contains(errText, "Priority: P1") ||
		!strings.Contains(errText, "Assignee: alice") ||
		!strings.Contains(errText, "View:     bd show bd-1234") ||
		!strings.Contains(errText, "Claim:    bd update bd-1234 --claim") {
		t.Fatalf("missing conflict details in error: %s", errText)
	}

	// Order check: Conflict first, then bypass flags, then config key
	idxConflict := strings.Index(errText, "Conflict:")
	idxBypass := strings.Index(errText, "To bypass this check:")
	idxAllowDup := strings.Index(errText, "--allow-duplicate")
	idxForce := strings.Index(errText, "--force")
	idxConfig := strings.Index(errText, "create.dedup-check")

	if idxConflict == -1 || idxBypass == -1 || idxAllowDup == -1 || idxForce == -1 || idxConfig == -1 {
		t.Fatalf("missing required section markers in error: %s", errText)
	}

	if !(idxConflict < idxBypass && idxBypass < idxAllowDup && idxAllowDup < idxForce && idxForce < idxConfig) {
		t.Fatalf("incorrect section ordering in error text; expected conflict < bypass flags < config key:\n%s", errText)
	}
}

// TestCreateDedup_IsDedupCheckEnabled verifies config resolution.
func TestCreateDedup_IsDedupCheckEnabled(t *testing.T) {
	clearDedupCheckEnv(t)
	_ = config.Initialize()

	// Default is true when unset
	config.Set("create.dedup-check", nil)
	if !isDedupCheckEnabled() {
		t.Errorf("expected isDedupCheckEnabled() to be true by default")
	}

	config.Set("create.dedup-check", false)
	if isDedupCheckEnabled() {
		t.Errorf("expected isDedupCheckEnabled() to be false when set to false")
	}

	config.Set("create.dedup-check", true)
	if !isDedupCheckEnabled() {
		t.Errorf("expected isDedupCheckEnabled() to be true when set to true")
	}
}

// TestCreateDedup_CLI_BlockedByDefaultAndBypasses tests duplicate prevention
// in live CLI operations with embedded Dolt:
// 1. Duplicate blocked by default with informative error
// 2. Bypass with --allow-duplicate
// 3. Bypass with --force
// 4. Bypass with config create.dedup-check false
// 5. Unique titles passing
// 6. Distinct migration bugs NOT blocked (>=2 domain tokens + 35% threshold)
// 7. --dry-run shows block without writing
// 8. Closed bead never blocks
func TestCreateDedup_CLI_BlockedByDefaultAndBypasses(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "dd")

	// 1. Create initial active issue
	initial := bdCreate(t, bd, dir, "Fix authentication token refresh bug", "--id", "dd-auth1")
	if initial.ID != "dd-auth1" {
		t.Fatalf("unexpected issue ID: %s", initial.ID)
	}

	// 2. Exact duplicate attempt should be blocked by default
	outFail := bdCreateFail(t, bd, dir, "Fix authentication token refresh bug")
	if !strings.Contains(outFail, "blocked: potential duplicate of active issue dd-auth1") {
		t.Fatalf("expected duplicate block message, got: %s", outFail)
	}
	if !strings.Contains(outFail, "--allow-duplicate") || !strings.Contains(outFail, "--force") || !strings.Contains(outFail, "create.dedup-check") {
		t.Fatalf("expected bypass guidance in error, got: %s", outFail)
	}

	// 3. Near-duplicate / paraphrased title should also be blocked
	outSimilar := bdCreateFail(t, bd, dir, "Authentication token refresh bug in client")
	if !strings.Contains(outSimilar, "blocked: potential duplicate of active issue dd-auth1") {
		t.Fatalf("expected similar duplicate block, got: %s", outSimilar)
	}

	// 4. --dry-run shows block without writing
	outDryRun := bdCreateFail(t, bd, dir, "Fix authentication token refresh bug", "--dry-run")
	if !strings.Contains(outDryRun, "blocked: potential duplicate of active issue dd-auth1") {
		t.Fatalf("expected dry-run to show duplicate block, got: %s", outDryRun)
	}

	// 5. Bypass with --allow-duplicate succeeds
	dupAllowed := bdCreate(t, bd, dir, "Fix authentication token refresh bug", "--allow-duplicate")
	if dupAllowed.Title != "Fix authentication token refresh bug" {
		t.Fatalf("expected issue to be created with --allow-duplicate, got: %+v", dupAllowed)
	}

	// 6. Bypass with --force succeeds
	dupForced := bdCreate(t, bd, dir, "Fix authentication token refresh bug", "--force")
	if dupForced.Title != "Fix authentication token refresh bug" {
		t.Fatalf("expected issue to be created with --force, got: %+v", dupForced)
	}

	// 7. Distinct migration bugs NOT blocked (fleet's real pattern):
	// Both share domain keywords like "database" and "migration", but have distinct scope.
	mig1 := bdCreate(t, bd, dir, "Database migration fails on table users")
	if mig1.Title != "Database migration fails on table users" {
		t.Fatalf("expected mig1 create to succeed: %+v", mig1)
	}
	mig2 := bdCreate(t, bd, dir, "Database migration performance degradation during bulk index creation")
	if mig2.Title != "Database migration performance degradation during bulk index creation" {
		t.Fatalf("expected mig2 create to succeed without false-positive block: %+v", mig2)
	}

	// 8. Unique title succeeds
	unique := bdCreate(t, bd, dir, "Completely different user interface task")
	if unique.Title != "Completely different user interface task" {
		t.Fatalf("expected unique issue to succeed: %+v", unique)
	}

	// 9. Closed bead never blocks (active-only scope is a hard rule)
	taskToClose := bdCreate(t, bd, dir, "Temporary one-off maintenance", "--id", "dd-close1")
	bdClose(t, bd, dir, taskToClose.ID)

	recreated := bdCreate(t, bd, dir, "Temporary one-off maintenance")
	if recreated.Title != "Temporary one-off maintenance" {
		t.Fatalf("expected creation after close to succeed without block: %+v", recreated)
	}

	// 10. Bypass via config create.dedup-check: false
	bdConfig(t, bd, dir, "set", "create.dedup-check", "false")
	dupWithConfig := bdCreate(t, bd, dir, "Fix authentication token refresh bug")
	if dupWithConfig.Title != "Fix authentication token refresh bug" {
		t.Fatalf("expected issue to be created when create.dedup-check is false, got: %+v", dupWithConfig)
	}
}

// TestCreateDedup_JSONOutput tests structured JSON output on blocked creation.
func TestCreateDedup_JSONOutput(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "dj")

	bdCreate(t, bd, dir, "Resolve memory leak in buffer pool", "--id", "dj-leak")

	cmd := exec.Command(bd, "create", "--json", "Resolve memory leak in buffer pool", "--due", "+7d")
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected bd create --json duplicate to exit non-zero")
	}

	var res map[string]interface{}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("failed to parse JSON error output: %v\nOutput: %s", err, string(out))
	}

	if _, ok := res["error"]; !ok {
		t.Fatalf("expected error field in JSON output: %s", string(out))
	}
	blocked, ok := res["blocked"].([]interface{})
	if !ok || len(blocked) != 1 {
		t.Fatalf("expected blocked array with 1 item in JSON: %s", string(out))
	}
	item := blocked[0].(map[string]interface{})
	if item["conflict_id"] != "dj-leak" {
		t.Fatalf("expected conflict_id dj-leak, got: %v", item["conflict_id"])
	}
	if item["conflict_title"] != "Resolve memory leak in buffer pool" {
		t.Fatalf("expected conflict_title match, got: %v", item["conflict_title"])
	}
}

// TestCreateDedup_BatchMarkdown tests markdown batch deduplication semantics:
// Fail-closed per-item: blocked items are NOT written, unblocked items ARE written,
// and exits non-zero if any item was blocked.
func TestCreateDedup_BatchMarkdown(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "bm")

	// Create active issue that will conflict
	bdCreate(t, bd, dir, "Existing batch conflict title", "--id", "bm-exist")

	// Create markdown file with 1 duplicate and 1 unique issue
	mdContent := `## Existing batch conflict title
### Type
task
### Priority
1
### Description
This issue is a duplicate.

## Brand new unique batch item
### Type
task
### Priority
2
### Description
This issue is unique.
`
	mdPath := filepath.Join(dir, "issues.md")
	if err := os.WriteFile(mdPath, []byte(mdContent), 0o600); err != nil {
		t.Fatalf("write markdown file: %v", err)
	}

	// 1. Text mode batch create: should exit non-zero, report blocked, and write unblocked
	cmd := exec.Command(bd, "create", "--file", mdPath)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected batch create with duplicate to exit non-zero")
	}
	outStr := string(out)
	if !strings.Contains(outStr, "Blocked 1 duplicate issue(s)") {
		t.Fatalf("expected Blocked duplicate issue message, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Created 1 issues") {
		t.Fatalf("expected partial creation report, got:\n%s", outStr)
	}

	// 2. Structured JSON batch create with one duplicate
	mdContentJSON := `## Existing batch conflict title
### Type
task

## Telemetry metrics collector pipeline
### Type
task
`
	mdPathJSON := filepath.Join(dir, "issues_json.md")
	if err := os.WriteFile(mdPathJSON, []byte(mdContentJSON), 0o600); err != nil {
		t.Fatalf("write markdown json file: %v", err)
	}

	cmdJSON := exec.Command(bd, "create", "--json", "--file", mdPathJSON)
	cmdJSON.Dir = dir
	cmdJSON.Env = bdEnv(dir)
	outJSON, errJSON := cmdJSON.CombinedOutput()
	if errJSON == nil {
		t.Fatalf("expected batch create --json to exit non-zero when blocked items exist")
	}

	var jsonRes struct {
		Created []string `json:"created"`
		Blocked []struct {
			ID            string  `json:"id"`
			ConflictID    string  `json:"conflict_id"`
			ConflictTitle string  `json:"conflict_title"`
			Similarity    float64 `json:"similarity"`
		} `json:"blocked"`
	}
	if err := json.Unmarshal(outJSON, &jsonRes); err != nil {
		t.Fatalf("failed to unmarshal JSON batch response: %v\nOutput: %s", err, string(outJSON))
	}

	if len(jsonRes.Blocked) != 1 || jsonRes.Blocked[0].ConflictID != "bm-exist" {
		t.Fatalf("unexpected blocked result in JSON: %+v", jsonRes.Blocked)
	}
	if len(jsonRes.Created) != 1 {
		t.Fatalf("expected 1 created issue in JSON, got: %+v", jsonRes.Created)
	}

	// 3. Batch with --allow-duplicate creates all
	cmdAllow := exec.Command(bd, "create", "--file", mdPath, "--allow-duplicate")
	cmdAllow.Dir = dir
	cmdAllow.Env = bdEnv(dir)
	outAllow, errAllow := cmdAllow.CombinedOutput()
	if errAllow != nil {
		t.Fatalf("expected batch create with --allow-duplicate to succeed: %v\n%s", errAllow, string(outAllow))
	}
	if !strings.Contains(string(outAllow), "Created 2 issues") {
		t.Fatalf("expected 2 issues created with --allow-duplicate: %s", string(outAllow))
	}
}

// TestCreateDedup_BatchGraph tests graph apply batch deduplication semantics:
// Per-item fail-closed: blocked nodes are not written, unblocked nodes are written,
// and exits non-zero if any item was blocked.
func TestCreateDedup_BatchGraph(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "bg")

	// Create active issue that will conflict
	bdCreate(t, bd, dir, "Existing graph node conflict", "--id", "bg-exist")

	graphPlan := `{
		"nodes": [
			{"key": "dup_node", "title": "Existing graph node conflict", "type": "task"},
			{"key": "unique_node", "title": "Brand new unique graph task", "type": "task"}
		]
	}`
	planPath := filepath.Join(dir, "graph_plan.json")
	if err := os.WriteFile(planPath, []byte(graphPlan), 0o600); err != nil {
		t.Fatalf("write graph plan: %v", err)
	}

	// 1. Text mode graph create: exits non-zero, blocked item reported, unblocked item written
	cmd := exec.Command(bd, "create", "--graph", planPath)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected graph create with duplicate to exit non-zero")
	}
	outStr := string(out)
	if !strings.Contains(outStr, "Blocked 1 duplicate issue(s)") {
		t.Fatalf("expected blocked graph node message, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Created 1 issues") {
		t.Fatalf("expected partial creation of unblocked node, got:\n%s", outStr)
	}

	// 2. Structured JSON mode graph create
	graphPlanJSON := `{
		"nodes": [
			{"key": "dup_node", "title": "Existing graph node conflict", "type": "task"},
			{"key": "unique_node_2", "title": "Telemetry metrics collector pipeline", "type": "task"}
		]
	}`
	planPathJSON := filepath.Join(dir, "graph_plan_json.json")
	if err := os.WriteFile(planPathJSON, []byte(graphPlanJSON), 0o600); err != nil {
		t.Fatalf("write graph plan json: %v", err)
	}

	cmdJSON := exec.Command(bd, "create", "--json", "--graph", planPathJSON)
	cmdJSON.Dir = dir
	cmdJSON.Env = bdEnv(dir)
	outJSON, errJSON := cmdJSON.CombinedOutput()
	if errJSON == nil {
		t.Fatalf("expected graph create --json with duplicate to exit non-zero")
	}

	var jsonRes struct {
		Created []string `json:"created"`
		Blocked []struct {
			ID            string  `json:"id"`
			ConflictID    string  `json:"conflict_id"`
			ConflictTitle string  `json:"conflict_title"`
			Similarity    float64 `json:"similarity"`
		} `json:"blocked"`
	}
	if err := json.Unmarshal(outJSON, &jsonRes); err != nil {
		t.Fatalf("failed to unmarshal JSON graph response: %v\nOutput: %s", err, string(outJSON))
	}
	if len(jsonRes.Blocked) != 1 || jsonRes.Blocked[0].ConflictID != "bg-exist" {
		t.Fatalf("unexpected blocked result in JSON: %+v", jsonRes.Blocked)
	}
	if len(jsonRes.Created) != 1 {
		t.Fatalf("expected 1 created node in JSON, got: %+v", jsonRes.Created)
	}

	// 3. Graph create with --allow-duplicate creates all nodes
	cmdAllow := exec.Command(bd, "create", "--graph", planPath, "--allow-duplicate")
	cmdAllow.Dir = dir
	cmdAllow.Env = bdEnv(dir)
	outAllow, errAllow := cmdAllow.CombinedOutput()
	if errAllow != nil {
		t.Fatalf("expected graph create with --allow-duplicate to succeed: %v\n%s", errAllow, string(outAllow))
	}
	if !strings.Contains(string(outAllow), "Created 2 issues") {
		t.Fatalf("expected 2 issues created with --allow-duplicate: %s", string(outAllow))
	}
}

// TestCreateDedup_RecurrencePrevention verifies Firstmate spec requirement 2 & 4:
// Closing a --repeat bead must not spawn an identical child when an active bead already covers the scope.
// A closed duplicate never blocks recurrence when no active cover exists.
func TestCreateDedup_RecurrencePrevention(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "rc")

	// 1. Create a recurring bead
	recurring := bdCreate(t, bd, dir, "Daily database backup verification", "--repeat", "+1d", "--id", "rc-rec1")
	if recurring.ID != "rc-rec1" {
		t.Fatalf("failed to create recurring bead: %+v", recurring)
	}

	// 2. Create an ACTIVE bead covering the same scope
	covering := bdCreate(t, bd, dir, "Daily database backup verification", "--id", "rc-active-cover", "--allow-duplicate")
	if covering.ID != "rc-active-cover" {
		t.Fatalf("failed to create covering bead: %+v", covering)
	}

	// 3. Close the recurring bead. Because rc-active-cover is active, recurrence duplicate prevention
	// must NOT spawn a new child!
	bdClose(t, bd, dir, "rc-rec1")

	// Check issue count: rc-rec1 (closed) + rc-active-cover (open) = exactly 2 issues, no child spawned
	listOut := bdRunWithFlockRetryBytes(t, bd, dir, "list", "--all", "--json")
	var allIssues []*types.Issue
	if err := json.Unmarshal(listOut, &allIssues); err != nil {
		t.Fatalf("failed to parse list output: %v", err)
	}
	if len(allIssues) != 2 {
		var titles []string
		for _, iss := range allIssues {
			titles = append(titles, iss.ID+": "+iss.Title+" ("+string(iss.Status)+")")
		}
		t.Fatalf("expected exactly 2 issues (no recurrence spawned due to active cover), found %d:\n%s",
			len(allIssues), strings.Join(titles, "\n"))
	}

	// The fold is recorded on the covering bead so it stays visible.
	commentsOut := bdRunWithFlockRetryBytes(t, bd, dir, "comments", "rc-active-cover", "--json")
	if !strings.Contains(string(commentsOut), "recurrence rc-rec1 folded into this active bead at") {
		t.Fatalf("expected fold audit comment on rc-active-cover, got:\n%s", commentsOut)
	}

	// 4. Now close the active cover bead as well
	bdClose(t, bd, dir, "rc-active-cover")

	// 5. Create another recurring bead with same scope now that all previous instances are closed
	_ = bdCreate(t, bd, dir, "Daily database backup verification", "--repeat", "+1d", "--id", "rc-rec2")

	// Close recurring2: since no ACTIVE bead covers the scope now (the old ones are closed),
	// the next recurrence instance MUST spawn!
	bdClose(t, bd, dir, "rc-rec2")

	listOutAfter := bdRunWithFlockRetryBytes(t, bd, dir, "list", "--all", "--json")
	var allIssuesAfter []*types.Issue
	if err := json.Unmarshal(listOutAfter, &allIssuesAfter); err != nil {
		t.Fatalf("failed to parse list output: %v", err)
	}
	// We expect: rc-rec1, rc-active-cover, rc-rec2, plus the spawned child! (4 issues)
	if len(allIssuesAfter) != 4 {
		var titles []string
		for _, iss := range allIssuesAfter {
			titles = append(titles, iss.ID+": "+iss.Title+" ("+string(iss.Status)+")")
		}
		t.Fatalf("expected 4 issues (spawned recurrence when no active cover), found %d:\n%s",
			len(allIssuesAfter), strings.Join(titles, "\n"))
	}
}

// bdRunWithFlockRetryBytes helper for tests.
func bdRunWithFlockRetryBytes(t *testing.T, bd, dir string, args ...string) []byte {
	t.Helper()
	out, err := bdRunWithFlockRetry(t, bd, dir, args...)
	if err != nil {
		t.Fatalf("command failed: %s %s: %v\n%s", bd, strings.Join(args, " "), err, string(out))
	}
	// Strip any warnings/tips before the JSON array/object
	s := string(out)
	idxArray := strings.Index(s, "[")
	idxObj := strings.Index(s, "{")
	start := -1
	if idxArray != -1 && (idxObj == -1 || idxArray < idxObj) {
		start = idxArray
	} else if idxObj != -1 {
		start = idxObj
	}
	if start != -1 {
		return []byte(s[start:])
	}
	return bytes.TrimSpace(out)
}

// TestCreateDedup_RecurrenceSimilarTitleStillSpawns: a merely similar active
// bead must not end a recurring series; only an identical title folds it.
func TestCreateDedup_RecurrenceSimilarTitleStillSpawns(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "rs")

	bdCreate(t, bd, dir, "Weekly security audit review", "--repeat", "+7d", "--id", "rs-weekly")
	bdCreate(t, bd, dir, "Security audit review of payment flow", "--id", "rs-payment", "--allow-duplicate")

	bdClose(t, bd, dir, "rs-weekly")

	listOut := bdRunWithFlockRetryBytes(t, bd, dir, "list", "--json")
	var open []*types.Issue
	if err := json.Unmarshal(listOut, &open); err != nil {
		t.Fatalf("failed to parse list output: %v\n%s", err, listOut)
	}
	spawned := false
	for _, iss := range open {
		if iss.Title == "Weekly security audit review" && iss.ID != "rs-weekly" {
			spawned = true
		}
	}
	if !spawned {
		t.Fatalf("expected recurrence successor despite similar active bead, got: %+v", open)
	}
}

// TestCreateDedup_GraphBlockedParentBlocksChildren: children (transitively)
// of a blocked graph node are blocked too, never created as orphans.
func TestCreateDedup_GraphBlockedParentBlocksChildren(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "gp")

	bdCreate(t, bd, dir, "Existing auth refresh epic", "--id", "gp-exist")

	plan := `{
		"nodes": [
			{"key": "epic", "title": "Existing auth refresh epic", "type": "epic"},
			{"key": "child", "title": "Write regression tests", "type": "task", "parent_key": "epic"},
			{"key": "grandchild", "title": "Seed fixture accounts", "type": "task", "parent_key": "child"},
			{"key": "other", "title": "Unrelated telemetry exporter", "type": "task"},
			{"key": "edgechild", "title": "Audit token storage layer", "type": "task"},
			{"key": "depchild", "title": "Document session renewal flow", "type": "task",
				"deps": [{"type": "parent-child", "target": "epic"}]}
		],
		"edges": [
			{"from_key": "other", "to_key": "child", "type": "blocks"},
			{"from_key": "edgechild", "to_key": "epic", "type": "parent-child"}
		]
	}`
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(plan), 0o600); err != nil {
		t.Fatalf("write graph plan: %v", err)
	}

	cmd := exec.Command(bd, "create", "--json", "--graph", planPath)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.Output()
	if err == nil {
		t.Fatalf("expected graph create with blocked nodes to exit non-zero")
	}
	var res struct {
		Created []string `json:"created"`
		Blocked []struct {
			ID            string `json:"id"`
			ConflictID    string `json:"conflict_id"`
			BlockedParent string `json:"blocked_parent"`
		} `json:"blocked"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, b := range res.Blocked {
		if b.ConflictID != "gp-exist" {
			t.Fatalf("blocked %s has conflict %s, want gp-exist", b.ID, b.ConflictID)
		}
		got[b.ID] = b.BlockedParent
	}
	want := map[string]string{"epic": "", "child": "epic", "grandchild": "child", "edgechild": "epic", "depchild": "epic"}
	if len(got) != len(want) {
		t.Fatalf("blocked = %+v, want %+v", got, want)
	}
	for k, v := range want {
		if p, ok := got[k]; !ok || p != v {
			t.Fatalf("blocked = %+v, want %+v", got, want)
		}
	}
	if len(res.Created) != 1 {
		t.Fatalf("expected only the unrelated node created, got %+v", res.Created)
	}

	listOut := bdRunWithFlockRetryBytes(t, bd, dir, "list", "--json")
	var issues []*types.Issue
	if err := json.Unmarshal(listOut, &issues); err != nil {
		t.Fatalf("parse list: %v\n%s", err, listOut)
	}
	for _, iss := range issues {
		switch iss.Title {
		case "Write regression tests", "Seed fixture accounts", "Audit token storage layer", "Document session renewal flow":
			t.Fatalf("child of blocked node was created: %s %s", iss.ID, iss.Title)
		}
	}
}

// TestCreateDedup_DryRunUnopenableRepoWarns: a dry-run against a --repo that
// cannot be opened still renders the preview, skipping the duplicate check.
func TestCreateDedup_DryRunUnopenableRepoWarns(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "dr")
	missing := filepath.Join(t.TempDir(), "not-a-repo")

	cmd := exec.Command(bd, "create", "--dry-run", "--repo", missing, "Preview only title")
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	stdout, stderr, err := runCommandBuffers(t, cmd)
	if err != nil {
		t.Fatalf("dry-run with unopenable repo failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "skipping duplicate check") {
		t.Fatalf("expected skip warning on stderr, got:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Preview only title") {
		t.Fatalf("expected dry-run preview, got:\n%s", stdout.String())
	}
}

// TestCreateDedup_PostInsertCollisionComment: when an identical-title active
// issue exists alongside a just-created one (the concurrent-create window),
// the new issue gets a comment naming the collision and neither is deleted.
func TestCreateDedup_PostInsertCollisionComment(t *testing.T) {
	clearDedupCheckEnv(t)
	tmpDir := t.TempDir()
	s := newTestStore(t, filepath.Join(tmpDir, ".beads", "beads.db"))
	ctx := context.Background()

	first := &types.Issue{Title: "Rotate signing keys", Priority: 2, IssueType: types.TypeTask, Status: types.StatusOpen}
	second := &types.Issue{Title: "rotate signing keys ", Priority: 2, IssueType: types.TypeTask, Status: types.StatusOpen}
	for _, iss := range []*types.Issue{first, second} {
		if err := s.CreateIssue(ctx, iss, "seat"); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	noteDuplicateCollisions(ctx, s, []*types.Issue{second}, false)

	comments, err := s.GetIssueComments(ctx, second.ID)
	if err != nil {
		t.Fatalf("get comments: %v", err)
	}
	if len(comments) != 1 || !strings.Contains(comments[0].Text, first.ID) ||
		!strings.Contains(comments[0].Text, "unique constraint") {
		t.Fatalf("expected one collision comment naming %s, got %+v", first.ID, comments)
	}
	for _, id := range []string{first.ID, second.ID} {
		if _, err := s.GetIssue(ctx, id); err != nil {
			t.Fatalf("issue %s missing after re-check: %v", id, err)
		}
	}

	noteDuplicateCollisions(ctx, s, []*types.Issue{second}, true)
	if after, _ := s.GetIssueComments(ctx, second.ID); len(after) != 1 {
		t.Fatalf("bypass must not add a collision comment, got %d comments", len(after))
	}
}

// TestCreateDedup_GraphEdgeOnBlockedNodeRemapsToConflict: a surviving node
// that depended on a blocked duplicate keeps that ordering against the active
// issue it duplicates, instead of silently becoming ready work.
func TestCreateDedup_GraphEdgeOnBlockedNodeRemapsToConflict(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "ge")

	bdCreate(t, bd, dir, "Existing auth refresh epic", "--id", "ge-exist")

	plan := `{
		"nodes": [
			{"key": "a", "title": "Existing auth refresh epic", "type": "task"},
			{"key": "b", "title": "Deploy telemetry exporter", "type": "task"},
			{"key": "c", "title": "Rotate staging certificates", "type": "task"}
		],
		"edges": [
			{"from_key": "b", "to_key": "a", "type": "blocks"},
			{"from_key": "a", "to_key": "c", "type": "blocks"}
		]
	}`
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(plan), 0o600); err != nil {
		t.Fatalf("write graph plan: %v", err)
	}
	cmd := exec.Command(bd, "create", "--json", "--graph", planPath)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.Output()
	if err == nil {
		t.Fatalf("expected graph create with a blocked node to exit non-zero")
	}
	var res struct {
		Created []string `json:"created"`
	}
	if err := json.Unmarshal(out, &res); err != nil || len(res.Created) != 2 {
		t.Fatalf("expected two created nodes, got %+v (err %v)\n%s", res.Created, err, out)
	}

	readyOut := string(bdRunWithFlockRetryBytes(t, bd, dir, "ready", "--json"))
	var readyIssues []*types.Issue
	if err := json.Unmarshal([]byte(readyOut), &readyIssues); err != nil {
		t.Fatalf("parse ready: %v\n%s", err, readyOut)
	}
	ready := map[string]string{}
	for _, iss := range readyIssues {
		ready[iss.Title] = iss.ID
	}
	if _, ok := ready["Deploy telemetry exporter"]; ok {
		t.Fatalf("dependent of the blocked node should stay blocked by ge-exist:\n%s", readyOut)
	}
	if _, ok := ready["Existing auth refresh epic"]; !ok {
		t.Fatalf("existing ge-exist must not gain a dependency from the blocked node's outgoing edge:\n%s", readyOut)
	}
	if _, ok := ready["Rotate staging certificates"]; !ok {
		t.Fatalf("expected the unrelated new node to be ready:\n%s", readyOut)
	}
}

// TestCreateDedup_RecheckIgnoresSameBatchSiblings: identical titles created
// together in one batch are not reported as a concurrent-create collision.
func TestCreateDedup_RecheckIgnoresSameBatchSiblings(t *testing.T) {
	clearDedupCheckEnv(t)
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "sb")

	plan := `{
		"nodes": [
			{"key": "one", "title": "Write regression tests", "type": "task"},
			{"key": "two", "title": "Write regression tests", "type": "task"}
		]
	}`
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(plan), 0o600); err != nil {
		t.Fatalf("write graph plan: %v", err)
	}
	out := bdRunWithFlockRetryBytes(t, bd, dir, "create", "--json", "--graph", planPath)
	var res GraphApplyResult
	if err := json.Unmarshal(out, &res); err != nil || len(res.IDs) != 2 {
		t.Fatalf("expected two created nodes, got %+v (err %v)\n%s", res.IDs, err, out)
	}
	for _, id := range res.IDs {
		comments := bdRunWithFlockRetryBytes(t, bd, dir, "comments", id, "--json")
		if strings.Contains(string(comments), "duplicate-title collision") {
			t.Fatalf("same-batch sibling reported as collision on %s:\n%s", id, comments)
		}
	}
}
