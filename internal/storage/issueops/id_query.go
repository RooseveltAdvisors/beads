package issueops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
// beads that exist at query time rather than from config, so a store that has
// never held a "use-after" bead can never read "use-after-free" as an ID.
// Config is deliberately NOT consulted: a configured-but-unused prefix has no
// rows for a text query to return, so reading it would change no result.
//
// CLASSIFY ONCE PER SEARCH, NOT ONCE PER LEG. planes lists every table plane
// the caller is about to query, and the first plane that carries the prefix
// settles it for all of them. A search that runs an issues leg and a wisps leg
// must hand both legs the SAME verdict — classifying each leg separately lets
// one leg run the narrow ID predicate while the other runs free text, and the
// merged result set then mixes ID semantics with prose semantics.
//
// Deriving the set is ONE bounded, sargable probe per plane rather than a
// DISTINCT scan over every id: the query names exactly one candidate prefix, so
// "SELECT 1 FROM <plane> WHERE id LIKE 'p-%' LIMIT 1" answers "is p in this
// plane's prefix set" with a single primary-key seek that stops at the first
// row. It runs only for a non-empty, ID-shaped query, so a prose search with a
// space or without a hyphen pays nothing at all, and it stops at the first
// plane that answers yes.
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
func ResolveIDQuery(ctx context.Context, tx DBTX, query string, filter types.IssueFilter, planes ...FilterTables) (types.IssueFilter, error) {
	if query == "" || filter.IDQuery || tx == nil || len(planes) == 0 {
		return filter, nil
	}
	prefix, ok := sqlbuild.IssueIDShape(query)
	if !ok {
		return filter, nil
	}
	for _, tables := range planes {
		used, err := storeUsesIDPrefix(ctx, tx, prefix, tables)
		if err != nil {
			return filter, err
		}
		if used {
			filter.IDQuery = true
			return filter, nil
		}
	}
	return filter, nil
}

// storeUsesIDPrefix reports whether any row in tables.Main carries an ID under
// prefix.
//
// ONLY TWO ANSWERS COUNT AS "no": sql.ErrNoRows, which is the probe working and
// finding nothing, and the absence of an optional wisp-plane table, which a
// database may legitimately not have. Every other error is returned, because a
// lock-wait timeout or a deadlock is indistinguishable from an empty result at
// the call site and answering "no" to it would silently downgrade an ID lookup
// to prose semantics — a wrong answer with no signal.
//
// The prefix is bound RAW (LIKE is case-sensitive on both backends) with its
// LIKE wildcards escaped, since a configured prefix may legitimately contain
// the single-character wildcard `_` (e.g. "bd_001").
func storeUsesIDPrefix(ctx context.Context, tx DBTX, prefix string, tables FilterTables) (bool, error) {
	var probe int
	err := tx.QueryRowContext(ctx,
		fmt.Sprintf("SELECT 1 FROM %s WHERE id LIKE ? ESCAPE '|' LIMIT 1", tables.Main),
		escapeLikeLiteral(prefix)+"-%",
	).Scan(&probe)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows), missingOptionalWispTable(err):
		return false, nil
	default:
		return false, fmt.Errorf("classify id query prefix %q against %s: %w", prefix, tables.Main, err)
	}
}

// escapeLikeLiteral neutralizes the LIKE wildcards in a literal operand for a
// clause that declares ESCAPE '|'.
func escapeLikeLiteral(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		switch c {
		case '%', '_', '|':
			b.WriteByte('|')
		}
		b.WriteRune(c)
	}
	return b.String()
}
