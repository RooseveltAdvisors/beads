package utils

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// resolverCall records one SearchIssueIDs invocation so the collaborator
// contract can be asserted.
type resolverCall struct {
	query  string
	filter types.IssueFilter
}

// scopedIDStore is a PartialIDResolverStore that answers ID-substring lookups
// and REFUSES an unscoped scan. Partial-ID resolution keeps only IDs that
// contain the search part, so binding it as a free-text query instead of
// IssueFilter.IDContains makes the backend LOWER() and scan every title and
// description body to produce rows the caller then throws away — the scan whose
// removal ResolvePartialID's own comment records as 60+ seconds on a 23k-issue
// store. Modelling that as an error keeps the regression observable here.
type scopedIDStore struct {
	prefix string
	ids    []string
	calls  []resolverCall
}

func (s *scopedIDStore) SearchIssues(_ context.Context, _ string, filter types.IssueFilter) ([]*types.Issue, error) {
	var out []*types.Issue
	for _, want := range filter.IDs {
		for _, id := range s.ids {
			if id == want {
				out = append(out, &types.Issue{ID: id})
			}
		}
	}
	return out, nil
}

func (s *scopedIDStore) SearchIssueIDs(_ context.Context, query string, filter types.IssueFilter) ([]string, error) {
	s.calls = append(s.calls, resolverCall{query: query, filter: filter})
	if filter.IDContains == "" {
		return nil, fmt.Errorf("unscoped id scan: partial-ID resolution must bind IDContains, got free-text query %q", query)
	}
	var out []string
	for _, id := range s.ids {
		if strings.Contains(strings.ToLower(id), strings.ToLower(filter.IDContains)) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *scopedIDStore) GetConfig(_ context.Context, key string) (string, error) {
	if key == "issue_prefix" {
		return s.prefix, nil
	}
	return "", nil
}

func TestResolvePartialIDBindsIDContainsNotAFreeTextQuery(t *testing.T) {
	store := &scopedIDStore{prefix: "bd", ids: []string{"bd-a3f8e9", "bd-b1c2d3"}}

	got, err := ResolvePartialID(context.Background(), store, "a3f8")
	if err != nil {
		t.Fatalf("ResolvePartialID(%q): %v", "a3f8", err)
	}
	if got != "bd-a3f8e9" {
		t.Fatalf("ResolvePartialID(%q) = %q, want %q", "a3f8", got, "bd-a3f8e9")
	}

	if len(store.calls) != 1 {
		t.Fatalf("expected 1 SearchIssueIDs call, got %d: %+v", len(store.calls), store.calls)
	}
	call := store.calls[0]
	if call.query != "" {
		t.Errorf("SearchIssueIDs query = %q, want empty: a free-text query scans title and description too", call.query)
	}
	if call.filter.IDContains != "a3f8" {
		t.Errorf("SearchIssueIDs filter.IDContains = %q, want %q", call.filter.IDContains, "a3f8")
	}
}

func TestResolvePartialIDWispFallbackBindsIDContains(t *testing.T) {
	store := &scopedIDStore{prefix: "bd", ids: []string{"bd-a3f8e9"}}

	if _, err := ResolvePartialID(context.Background(), store, "zzz9"); err == nil {
		t.Fatal("expected ResolvePartialID to report no match")
	}

	if len(store.calls) != 2 {
		t.Fatalf("expected the issues scan plus the wisp fallback, got %d calls: %+v", len(store.calls), store.calls)
	}
	wisp := store.calls[1]
	if wisp.query != "" {
		t.Errorf("wisp fallback query = %q, want empty", wisp.query)
	}
	if wisp.filter.IDContains != "zzz9" {
		t.Errorf("wisp fallback filter.IDContains = %q, want %q", wisp.filter.IDContains, "zzz9")
	}
	if wisp.filter.Ephemeral == nil || !*wisp.filter.Ephemeral {
		t.Errorf("wisp fallback should scope to the wisp plane, got Ephemeral %v", wisp.filter.Ephemeral)
	}
}
