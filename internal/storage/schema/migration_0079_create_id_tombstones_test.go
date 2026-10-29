package schema

import (
	"os"
	"strings"
	"testing"
)

const (
	migration0079Up        = "0079_create_id_tombstones.up.sql"
	migration0079Down      = "0079_create_id_tombstones.down.sql"
	ignoredMigration0033Up = "0033_create_wisp_id_tombstones.up.sql"
)

// wispIDTombstonesDDL is the one shape both doors must create the clone-local
// tombstone table with: main 0079 on an upgraded workspace, ignored/0033 on a
// fresh clone.
const wispIDTombstonesDDL = `CREATE TABLE IF NOT EXISTS wisp_id_tombstones (
    id VARCHAR(255) NOT NULL,
    closed_at DATETIME NOT NULL,
    PRIMARY KEY (id)
);`

// TestLatestVersionIncludesMigration0079 pins the slots this migration claims,
// main 0079 and ignored 0033, superseding 0077's own version of this test
// (migration_0075_add_epoch_minted_addresses_test.go; only one such pin lives at
// a time). Upstream numbered these 0074 and ignored 0030; the house migration
// freeze renumbers them above the house ceiling of 0077, so the live slots are
// 0079 and 0033. The slots sit directly above arrival_seq's (0078, ignored
// 0032). A hardcoded literal for the reason the earlier ones were: a drift to
// 79 for the wrong reason is caught by the shape test below failing to explain
// why 79 is tombstone-shaped.
func TestLatestVersionIncludesMigration0079(t *testing.T) {
	const want = 79
	if got := LatestVersion(); got != want {
		t.Fatalf("LatestVersion() = %d, want %d (id tombstone tables)", got, want)
	}
	const wantIgnored = 33
	if got := LatestIgnoredVersion(); got != wantIgnored {
		t.Fatalf("LatestIgnoredVersion() = %d, want %d (wisp_id_tombstones on fresh clones)", got, wantIgnored)
	}
}

// TestMigration0079CreatesBothTombstoneTables is a DB-independent check of the
// frozen bytes: both tables created with plain IF NOT EXISTS (so the CLI
// bundle needs no override — nothing is PREPAREd), the clone-local one in the
// exact shape ignored/0033 materializes, and a down migration that drops both.
// The engine-level behavior is pinned by the embedded store's 0079 test.
func TestMigration0079CreatesBothTombstoneTables(t *testing.T) {
	upSQL, err := MigrationSQL(migration0079Up)
	if err != nil {
		t.Fatalf("MigrationSQL(%s) = %v", migration0079Up, err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS id_tombstones (\n    id VARCHAR(255) NOT NULL,\n    closed_at DATETIME NOT NULL,\n    PRIMARY KEY (id)\n);",
		wispIDTombstonesDDL,
	} {
		if !strings.Contains(upSQL, want) {
			t.Errorf("0074 up migration missing %q", want)
		}
	}
	if strings.Contains(strings.ToUpper(upSQL), "PREPARE STMT") || strings.Contains(strings.ToUpper(upSQL), "EXECUTE STMT") {
		t.Error("0074 must not PREPARE anything: the pre-2.3 Dolt CLI bundle path silently no-ops prepared statements")
	}
	if got := cliCompatibleMigrationSQL(migration0079Up, upSQL); got != upSQL {
		t.Error("0074 carries a CLI bundle override; it needs none")
	}

	ignoredSQL, err := IgnoredMigrationSQL(ignoredMigration0033Up)
	if err != nil {
		t.Fatalf("IgnoredMigrationSQL(%s) = %v", ignoredMigration0033Up, err)
	}
	if !strings.Contains(ignoredSQL, wispIDTombstonesDDL) {
		t.Errorf("ignored/0033 does not create wisp_id_tombstones in 0079's exact shape %q", wispIDTombstonesDDL)
	}

	downBytes, err := os.ReadFile("migrations/" + migration0079Down)
	if err != nil {
		t.Fatalf("read %s: %v", migration0079Down, err)
	}
	for _, want := range []string{"DROP TABLE IF EXISTS id_tombstones;", "DROP TABLE IF EXISTS wisp_id_tombstones;"} {
		if !strings.Contains(string(downBytes), want) {
			t.Errorf("0074 down migration missing %q", want)
		}
	}
}
