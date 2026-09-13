package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/metrics"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/ui"
)

// scheduledSweeper is the capability `bd due sweep` needs: run the two lazy
// time-based sweeps NOW and say what fired.
//
// It is reached by walking Unwrap() rather than by widening storage.Storage,
// the same way storeExportSource reaches the wisp partitioner: the capability
// belongs to the two concrete stores that own a transaction, and every
// decorator between here and them (telemetry, hook firing) has nothing to add
// to a sweep — there is no create, update or close hook for a deadline
// arriving, only the audit event the sweep already writes.
type scheduledSweeper interface {
	RunScheduledSweeps(ctx context.Context) (issueops.ScheduledSweepResult, error)
}

// dueSweepSeat is one assignee bucket on a sweep report. The external clock
// routes each seat's ids to that seat's wake rail (herdr session, inbox,
// firstmate queue) without re-querying the store.
type dueSweepSeat struct {
	Assignee string   `json:"assignee"`
	IDs      []string `json:"ids"`
}

// dueSweepReport is what one sweep did. It is the timer's payload: counts to
// put in a summary line, and ids so a human reading the line can go look.
type dueSweepReport struct {
	SweptAt      string   `json:"swept_at"`
	Summary      string   `json:"summary"`
	DueFired     int      `json:"due_fired"`
	DueIDs       []string `json:"due_ids,omitempty"`
	DueWisps     int      `json:"due_wisps"`
	Escalated    int      `json:"escalated"`
	EscalatedIDs []string `json:"escalated_ids,omitempty"`
	// EscalatedWisps is reported separately rather than folded into Escalated
	// for the reason DueWisps is: the two planes are different namespaces, and
	// a count that mixed them would make a wisp-plane escalation look like
	// work a human should go read.
	EscalatedWisps int      `json:"escalated_wisps"`
	DefersWoken    int      `json:"defers_woken"`
	DeferIDs       []string `json:"defer_ids,omitempty"`
	DeferWisps     int      `json:"defer_wisps"`
	// ByAssignee groups due + escalated issue ids by assignee seat. Empty
	// assignee becomes "unassigned". Publishers fan out per seat so the
	// assignee field is the routing key, not a decoration.
	ByAssignee []dueSweepSeat `json:"by_assignee,omitempty"`
}

// Summary is the one line an external clock publishes. It names both sweeps
// because they run in one pass and a reader who sees only the due half cannot
// tell a quiet clock from a half-broken one.
func (r dueSweepReport) summaryLine() string {
	return fmt.Sprintf("%d due, %d escalated, %d defer(s) woken",
		r.DueFired, r.Escalated, r.DefersWoken)
}

var dueSweepCmd = &cobra.Command{
	Use:   "sweep",
	Short: "Fire every bead whose due date has arrived",
	Long: `Run the due-date sweep now and report what fired.

Beads fire lazily on ready-work reads, which is a latency FLOOR, not a clock:
a workspace nobody reads never fires anything. This command is the seam an
external clock stands on — a timer runs it on a fixed cadence, reads the
summary, and publishes it onto whatever rail consumes due beads.

Each bead whose due date has arrived records a 'due' audit event (the rail
'bd events' already carries) and has its due date moved forward, so it nags
again rather than going silent: to its next occurrence if it repeats, and one
grace interval otherwise. Expired dated defers wake in the same pass, because
both are time-based sweeps and sharing the transaction costs one round trip
instead of two.

Unlike the lazy sweep behind a ready read, this one is NOT advisory: a sweep
that could not run exits non-zero, so a clock can tell a quiet workspace from
a broken one.

Examples:
  bd due sweep                         # fire what is due, print a summary
  bd due sweep --json                  # same, machine-readable`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		evt := metrics.NewCommandEvent("due-sweep")
		defer func() {
			if c := metrics.Global(); c != nil {
				c.CloseEventAndAdd(evt)
			}
		}()

		// Proxied mode is refused rather than half-supported, exactly as
		// `bd due backfill` refuses it. The server runs this sweep itself on
		// every ready read it serves, so a proxied client asking for one is
		// asking the wrong process: the clock belongs beside the database,
		// where a failure is visible and a summary means something.
		if usesProxiedServer() {
			return HandleErrorRespectJSON("due sweep is not supported in proxied-server mode\n  The server sweeps on its own reads. Run the clock against the database directly (a local `bd` in the workspace, or on the server host).")
		}

		if store == nil {
			return HandleErrorWithHint("database not initialized", diagHint())
		}
		CheckReadonly("due sweep")

		sweeper, ok := findScheduledSweeper(store)
		if !ok {
			return HandleErrorRespectJSON("this storage backend cannot run a due sweep on demand")
		}

		swept, err := sweeper.RunScheduledSweeps(rootCtx)
		if err != nil {
			return HandleError("due sweep: %v", err)
		}

		report := dueSweepReport{
			SweptAt:        time.Now().UTC().Format(time.RFC3339),
			DueFired:       len(swept.Due.Issues),
			DueIDs:         swept.Due.Issues,
			DueWisps:       len(swept.Due.Wisps),
			Escalated:      len(swept.Due.Escalated),
			EscalatedIDs:   swept.Due.Escalated,
			EscalatedWisps: len(swept.Due.EscalatedWisps),
			DefersWoken:    len(swept.Defers.Issues),
			DeferIDs:       swept.Defers.Issues,
			DeferWisps:     len(swept.Defers.Wisps),
		}
		// Seat routing is part of the sweep payload, not a publisher-side
		// re-query. Assignees are looked up once here so every rail sees the
		// same grouping.
		report.ByAssignee = groupDueIDsByAssignee(rootCtx, store, append(append([]string{}, report.DueIDs...), report.EscalatedIDs...))
		// The summary is a FIELD, not only a rendering, so an external clock
		// can publish the line with one grep instead of reassembling it from
		// counts — and so the line a human reads and the line a rail carries
		// are the same string.
		report.Summary = report.summaryLine()
		// Beads-owned delivery: enqueue seat-addressed outbox rows so drains
		// do not depend on firstmate or herdr. Failure here is logged inside
		// enqueueDueSweepNotifies and never fails the clock.
		enqueueDueSweepNotifies(report)
		if jsonOutput {
			return printJSON(report)
		}
		printDueSweepReport(report)
		return nil
	},
}

// groupDueIDsByAssignee builds the seat map a publisher fans out from.
// Unknown ids land under "unassigned" rather than vanishing: a missing seat
// is still a routing problem the clock must surface.
func groupDueIDsByAssignee(ctx context.Context, s storage.DoltStorage, ids []string) []dueSweepSeat {
	if len(ids) == 0 || s == nil {
		return nil
	}
	seen := make(map[string]bool, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	buckets := map[string][]string{}
	for _, id := range unique {
		seat := "unassigned"
		if issue, err := s.GetIssue(ctx, id); err == nil && issue != nil {
			if a := strings.TrimSpace(issue.Assignee); a != "" {
				seat = a
			}
		}
		buckets[seat] = append(buckets[seat], id)
	}
	seats := make([]string, 0, len(buckets))
	for seat := range buckets {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	out := make([]dueSweepSeat, 0, len(seats))
	for _, seat := range seats {
		out = append(out, dueSweepSeat{Assignee: seat, IDs: buckets[seat]})
	}
	return out
}

// findScheduledSweeper walks the decorator chain down to the store that can
// run a sweep. The store global is decorator-wrapped (telemetry, hook firing);
// Unwrap() is how every other capability reaches past them.
func findScheduledSweeper(s storage.DoltStorage) (scheduledSweeper, bool) {
	for s != nil {
		if sweeper, ok := s.(scheduledSweeper); ok {
			return sweeper, true
		}
		u, ok := s.(interface{ Unwrap() storage.DoltStorage })
		if !ok {
			return nil, false
		}
		s = u.Unwrap()
	}
	return nil, false
}

func printDueSweepReport(r dueSweepReport) {
	fmt.Printf("%s %s\n", ui.RenderAccent("*"), r.summaryLine())
	escalated := make(map[string]bool, len(r.EscalatedIDs))
	for _, id := range r.EscalatedIDs {
		escalated[id] = true
	}
	for _, id := range r.DueIDs {
		if escalated[id] {
			fmt.Printf("  due   %s %s\n", id, ui.RenderWarn("(escalated)"))
			continue
		}
		fmt.Printf("  due   %s\n", id)
	}
	for _, id := range r.DeferIDs {
		fmt.Printf("  woken %s\n", id)
	}
	for _, seat := range r.ByAssignee {
		fmt.Printf("  seat  %s (%d)\n", seat.Assignee, len(seat.IDs))
	}
	if r.DueWisps > 0 || r.DeferWisps > 0 || r.EscalatedWisps > 0 {
		fmt.Printf("  (%d due, %d escalated, %d woken in the wisp plane)\n",
			r.DueWisps, r.EscalatedWisps, r.DeferWisps)
	}
}

func init() {
	dueCmd.AddCommand(dueSweepCmd)
}
