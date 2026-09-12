package uow

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/steveyegge/beads/internal/types"
	publicops "github.com/steveyegge/beads/issueops"
)

// The unit-of-work update leg is what the daemon and the proxied transports
// run, so it must carry the recurrence patch like the classic funnel does and
// treat a status crossing into closed as a close: a series that silently
// stops on this leg never recurs at all, whatever the embedded backend does.
func TestIssueOperationsUpdateCarriesRecurrenceAndSpawnsOnClose(t *testing.T) {
	ctx := context.Background()
	provider := newUOWRoleFixtureProvider(t, ctx, "uwr")
	source, ok := provider.(IssueLifecycleSource)
	require.True(t, ok, "provider %T does not offer the IssueLifecycle accessor", provider)
	lifecycle, err := source.IssueLifecycle()
	require.NoError(t, err)

	due := time.Now().UTC().AddDate(0, 0, -1).Truncate(time.Minute)
	created, err := lifecycle.Create(ctx, publicops.CreateRequest{
		Actor: "tester",
		Issue: &publicops.Issue{
			Title:         "uow recurring",
			IssueType:     types.TypeChore,
			Priority:      2,
			DueAt:         &due,
			DueSource:     types.DueSourceExplicit,
			RepeatStart:   &due,
			RepeatPattern: "+1w",
		},
	})
	require.NoError(t, err)
	id := created.Issue.ID

	// A recurrence patch persists. It used to be dropped wholesale on this
	// leg: the request returned Changed=false and the row kept its old bounds.
	newEnd := due.AddDate(0, 0, 30)
	updated, err := lifecycle.Update(ctx, publicops.UpdateRequest{
		Actor:   "tester",
		IssueID: id,
		Patch: publicops.IssuePatch{
			RepeatEnd: publicops.Field[*time.Time]{Set: true, Value: &newEnd},
		},
	})
	require.NoError(t, err)
	require.True(t, updated.Changed, "recurrence patch reported no change")
	require.NotNil(t, updated.Issue.RepeatEnd, "repeat_end did not land")
	require.True(t, updated.Issue.RepeatEnd.Equal(newEnd),
		"repeat_end = %v, want %v", updated.Issue.RepeatEnd, newEnd)

	// A landing shape no create would accept is refused before anything
	// writes: an unparseable pattern cannot reach the row on this leg.
	_, err = lifecycle.Update(ctx, publicops.UpdateRequest{
		Actor:   "tester",
		IssueID: id,
		Patch: publicops.IssuePatch{
			RepeatPattern: publicops.Field[string]{Set: true, Value: "every tuesday"},
		},
	})
	require.Error(t, err, "an unparseable repeat pattern must be refused")

	// Closing by status spawns the successor inside the same unit of work:
	// the series must not end because the caller typed an update instead of
	// `bd close`.
	closed, err := lifecycle.Update(ctx, publicops.UpdateRequest{
		Actor:   "tester",
		IssueID: id,
		Patch: publicops.IssuePatch{
			Status: publicops.Field[publicops.Status]{Set: true, Value: types.StatusClosed},
		},
	})
	require.NoError(t, err)
	require.Equal(t, types.StatusClosed, closed.Issue.Status)

	successor, err := RunTxRead(ctx, provider, func(ctx context.Context, uw UnitOfWork) (*types.Issue, error) {
		result, err := uw.RawSQLUseCase().Query(ctx,
			"SELECT id FROM issues WHERE title = ? AND id <> ?", "uow recurring", id)
		if err != nil {
			return nil, err
		}
		if len(result.Rows) != 1 {
			return nil, fmt.Errorf("want exactly one spawned successor, got %d rows", len(result.Rows))
		}
		return uw.IssueUseCase().GetIssue(ctx, fmt.Sprint(result.Rows[0][0]))
	})
	require.NoError(t, err)
	require.Equal(t, types.StatusOpen, successor.Status, "successor was not born open")
	require.Equal(t, "+1w", successor.RepeatPattern, "successor lost the series' pattern")
	require.NotNil(t, successor.DueAt, "successor has no due date")
	if successor.DueAt != nil {
		require.True(t, successor.DueAt.After(time.Now().UTC()),
			"successor due %v is not in the future: born overdue", successor.DueAt)
		require.True(t, successor.DueAt.Equal(due.AddDate(0, 0, 7)),
			"successor due %v, want the next weekly occurrence %v", successor.DueAt, due.AddDate(0, 0, 7))
	}
}
