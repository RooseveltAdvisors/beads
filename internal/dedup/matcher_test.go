package dedup

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestMatcher(t *testing.T) {
	activeIssues := []*types.Issue{
		{
			ID:       "bd-1",
			Title:    "Daily standup and sync",
			Status:   types.StatusOpen,
			Priority: 2,
			Assignee: "alice",
		},
		{
			ID:       "bd-2",
			Title:    "Database migration fails on table users with foreign key error",
			Status:   types.StatusInProgress,
			Priority: 1,
			Assignee: "bob",
		},
		{
			ID:       "bd-3",
			Title:    "Fix auth token refresh in client",
			Status:   types.StatusBlocked,
			Priority: 1,
			Assignee: "carol",
		},
		{
			ID:       "bd-4",
			Title:    "Historical closed bug",
			Status:   types.StatusClosed,
			Priority: 2,
			Assignee: "",
		},
	}

	matcher := NewMatcher(activeIssues)

	t.Run("same_title_recurrence_style_blocked", func(t *testing.T) {
		conflict := matcher.FindConflict("Daily standup and sync")
		if conflict == nil {
			t.Fatal("expected conflict for identical recurrence title, got nil")
		}
		if conflict.ConflictIssue.ID != "bd-1" {
			t.Errorf("conflict ID = %q, want bd-1", conflict.ConflictIssue.ID)
		}
		if conflict.Similarity < 0.99 {
			t.Errorf("similarity = %f, want ~1.0", conflict.Similarity)
		}
	})

	t.Run("paraphrased_high_similarity_blocked", func(t *testing.T) {
		conflict := matcher.FindConflict("Client auth token refresh fix")
		if conflict == nil {
			t.Fatal("expected conflict for paraphrased auth token issue, got nil")
		}
		if conflict.ConflictIssue.ID != "bd-3" {
			t.Errorf("conflict ID = %q, want bd-3", conflict.ConflictIssue.ID)
		}
		if conflict.Similarity < SimilarityThreshold {
			t.Errorf("similarity = %f, want >= %f", conflict.Similarity, SimilarityThreshold)
		}
	})

	t.Run("containment_with_domain_keywords_blocked", func(t *testing.T) {
		conflict := matcher.FindConflict("Fix auth token refresh")
		if conflict == nil {
			t.Fatal("expected conflict for containment title, got nil")
		}
		if conflict.ConflictIssue.ID != "bd-3" {
			t.Errorf("conflict ID = %q, want bd-3", conflict.ConflictIssue.ID)
		}
	})

	t.Run("deliberately_distinct_second_bead_with_shared_vocabulary_not_blocked", func(t *testing.T) {
		// Both share "database" and "migration", but have distinct vocabulary and < 35% similarity.
		title := "Database migration performance degradation during bulk index creation"
		conflict := matcher.FindConflict(title)
		if conflict != nil {
			t.Fatalf("expected distinct migration bug NOT to be blocked, but got conflict with %s (sim=%f, reason=%s)",
				conflict.ConflictIssue.ID, conflict.Similarity, conflict.Reason)
		}
	})

	t.Run("single_shared_domain_token_not_blocked", func(t *testing.T) {
		// Shares only "migration" (< 2 domain tokens)
		title := "Kafka topic migration to new partition scheme"
		conflict := matcher.FindConflict(title)
		if conflict != nil {
			t.Fatalf("expected 1-token overlap NOT to be blocked, got conflict with %s", conflict.ConflictIssue.ID)
		}
	})

	t.Run("closed_issue_never_blocks", func(t *testing.T) {
		// bd-4 is closed; should not be indexed or matched
		conflict := matcher.FindConflict("Historical closed bug")
		if conflict != nil {
			t.Fatalf("closed issue must never block creation, got conflict with %s", conflict.ConflictIssue.ID)
		}
	})

	t.Run("unique_title_passes", func(t *testing.T) {
		conflict := matcher.FindConflict("Implement webhooks for stripe payments")
		if conflict != nil {
			t.Fatalf("expected unique title not to be blocked, got conflict with %s", conflict.ConflictIssue.ID)
		}
	})
}

func TestIsContainmentDirect(t *testing.T) {
	dA := ExtractDomainTokens(Tokenize("Database migration performance degradation during bulk index creation"))
	dB := ExtractDomainTokens(Tokenize("Database migration fails on table users with foreign key error"))
	t.Logf("dA: %v", dA)
	t.Logf("dB: %v", dB)
	t.Logf("IsContainment(dA, dB): %v", IsContainment(dA, dB))
}
