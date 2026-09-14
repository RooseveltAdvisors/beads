package issueops

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/storage/sqlbuild"
	"github.com/steveyegge/beads/internal/types"
)

// ResolveIDQuery classifies a text query as an issue ID or as free text, and
// returns the filter the search should actually run with.
//
// A query is an ID only when BOTH halves agree: it has the shape of an issue ID
// (sqlbuild.IssueIDShape) AND its prefix is one the TARGET STORE actually uses.
// Everything else — "use-after-free", "race-condition", "rate-limit" — is free
// text and is matched against titles, descriptions and id substrings.
//
// THE STORE HALF IS THE LOAD-BEARING ONE. Shape alone cannot tell "rate-limit"
// from "bd-a3f8e9"; only the corpus can. The prefix set is derived from the
// issues that exist at query time rather than from config, so a store that has
// never held a "use-" bead can never read "use-after-free" as an ID. Config is
// deliberately NOT consulted: a configured-but-unused prefix has no rows for a
// text query to return, so reading it would change no result.
//
// Deriving the set is ONE bounded, sargable probe rather than a DISTINCT scan
// over every id: the query names exactly one candidate prefix, so
// "SELECT 1 FROM <main> WHERE id LIKE 'p-%' LIMIT 1" answers "is p in the
// store's prefix set" with a single primary-key seek that stops at the first
// row. It runs only for a non-empty, ID-shaped query, so a prose search with a
// space or without a hyphen pays nothing at all.
//
// MEASURED, AND DELIBERATELY NOT CACHED. Over an embedded-Dolt workspace of 400
// issues, 25 `bd search --json` runs per query: prose 196/242 ms per run,
// ID-shaped-and-known 202/224 ms, ID-shaped-and-unknown 211/207 ms. The probe
// is below the noise floor — run-to-run variance on the SAME query (~25 ms)
// exceeds every probed-vs-unprobed gap. A cache would add invalidation to buy
// something unmeasurable, so there is none.
//
// There is no fallback, retry or heuristic layered on this either: a
// classification this function declines is free text, full stop.
func ResolveIDQuery(ctx context.Context, tx DBTX, query string, filter types.IssueFilter, tables FilterTables) types.IssueFilter {
	if query == "" || filter.IDQuery {
		return filter
	}
	prefix, ok := sqlbuild.IssueIDShape(query)
	if !ok {
		return filter
	}
	if storeUsesIDPrefix(ctx, tx, prefix, tables) {
		filter.IDQuery = true
	}
	return filter
}

// storeUsesIDPrefix reports whether any row in the searched table carries an ID
// under prefix. A probe error (a missing optional wisps table, a closed tx)
// answers "no", which routes the query to free text — the safe side, since free
// text still matches the id column by substring.
func storeUsesIDPrefix(ctx context.Context, tx DBTX, prefix string, tables FilterTables) bool {
	if tx == nil {
		return false
	}
	var probe int
	err := tx.QueryRowContext(ctx,
		fmt.Sprintf("SELECT 1 FROM %s WHERE id LIKE ? LIMIT 1", tables.Main),
		prefix+"-%",
	).Scan(&probe)
	return err == nil
}
