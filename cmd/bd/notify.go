package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/notify"
	"github.com/steveyegge/beads/internal/ui"
)

// notifyCmd is the beads-owned delivery surface. Time (due sweep) enqueues;
// this command drains seat outboxes without firstmate or herdr in the core path.
var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Seat-addressed notify outbox (beads-owned delivery)",
	Long: `The notify outbox is how beads delivers due/repeat fires to assignee seats.

bd due sweep enqueues one row per fired bead under its assignee seat
(.beads/notify/). bd notify drain pulls pending rows for a seat and either
prints them or runs a transport command. Firstmate and herdr are optional
transports - not the bus.

Examples:
  bd notify pending
  bd notify pending --seat wiseman --json
  bd notify drain --seat wiseman
  bd notify drain --seat wiseman --exec 'herdr --session wiseman agent prompt w1:p1 "$BD_NOTIFY_PROMPT"'
  bd notify ack --seat wiseman --upto 12
  bd notify seats`,
}

var notifyPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "List undelivered notify rows",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		seat, _ := cmd.Flags().GetString("seat")
		o, err := openNotifyOutbox()
		if err != nil {
			return err
		}
		pending, err := o.Pending(seat)
		if err != nil {
			return HandleError("%v", err)
		}
		if jsonOutput {
			return printJSON(pending)
		}
		if len(pending) == 0 {
			fmt.Printf("%s no pending notifies\n", ui.RenderAccent("*"))
			return nil
		}
		for _, r := range pending {
			fmt.Printf("  %d  %-12s  %-8s  %s  %s\n", r.Seq, r.Seat, r.Kind, r.IssueID, r.Title)
		}
		fmt.Printf("%s %d pending\n", ui.RenderAccent("*"), len(pending))
		return nil
	},
}

var notifySeatsCmd = &cobra.Command{
	Use:   "seats",
	Short: "List seats that have appeared in the outbox",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		o, err := openNotifyOutbox()
		if err != nil {
			return err
		}
		seats, err := o.Seats()
		if err != nil {
			return HandleError("%v", err)
		}
		if jsonOutput {
			return printJSON(seats)
		}
		for _, s := range seats {
			pending, _ := o.Pending(s)
			fmt.Printf("  %-16s  pending=%d\n", s, len(pending))
		}
		return nil
	},
}

var notifyDrainCmd = &cobra.Command{
	Use:   "drain",
	Short: "Pull pending rows for a seat and print or exec a transport",
	Long: `Drain pending notify rows for one seat.

Without --exec, prints each row (or JSON with --json) and acks them unless
--no-ack is set.

With --exec CMD, runs CMD once per row with environment:
  BD_NOTIFY_SEQ BD_NOTIFY_SEAT BD_NOTIFY_ID BD_NOTIFY_KIND BD_NOTIFY_TITLE
  BD_NOTIFY_PROMPT  a ready-to-send one-line prompt for agent transports
  BD_NOTIFY_JSON    the full record as JSON

The command's exit status must be 0 to count as delivered. Failed rows are
left pending. Successful rows are acked up to the highest contiguous success
unless --no-ack.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		seat, _ := cmd.Flags().GetString("seat")
		if strings.TrimSpace(seat) == "" {
			return HandleError("notify drain requires --seat")
		}
		limit, _ := cmd.Flags().GetInt("limit")
		execCmd, _ := cmd.Flags().GetString("exec")
		noAck, _ := cmd.Flags().GetBool("no-ack")

		o, err := openNotifyOutbox()
		if err != nil {
			return err
		}
		pending, err := o.Pending(seat)
		if err != nil {
			return HandleError("%v", err)
		}
		if limit > 0 && len(pending) > limit {
			pending = pending[:limit]
		}
		if len(pending) == 0 {
			if jsonOutput {
				return printJSON([]notify.Record{})
			}
			fmt.Printf("%s seat %s: nothing pending\n", ui.RenderAccent("*"), seat)
			return nil
		}

		var delivered []notify.Record
		var lastOK int64
		for _, rec := range pending {
			if execCmd != "" {
				if err := runNotifyExec(execCmd, rec); err != nil {
					fmt.Fprintf(os.Stderr, "notify drain: seq %d %s: %v\n", rec.Seq, rec.IssueID, err)
					break // stop so ack stays contiguous
				}
			} else if jsonOutput {
				// collect for batch print below
			} else {
				fmt.Printf("  %d  %s  %s  %s\n", rec.Seq, rec.Kind, rec.IssueID, rec.Title)
			}
			delivered = append(delivered, rec)
			lastOK = rec.Seq
		}

		if execCmd == "" && jsonOutput {
			if err := printJSON(delivered); err != nil {
				return err
			}
		}

		if !noAck && lastOK > 0 {
			if err := o.AckSeat(seat, lastOK); err != nil {
				return HandleError("ack: %v", err)
			}
		}
		if !jsonOutput {
			fmt.Printf("%s seat %s: delivered %d (acked through %d)\n",
				ui.RenderAccent("*"), seat, len(delivered), lastOK)
		}
		return nil
	},
}

var notifyAckCmd = &cobra.Command{
	Use:   "ack",
	Short: "Mark notify rows delivered for a seat up to a seq",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		seat, _ := cmd.Flags().GetString("seat")
		upto, _ := cmd.Flags().GetInt64("upto")
		if strings.TrimSpace(seat) == "" || upto <= 0 {
			return HandleError("notify ack requires --seat and --upto")
		}
		o, err := openNotifyOutbox()
		if err != nil {
			return err
		}
		if err := o.AckSeat(seat, upto); err != nil {
			return HandleError("%v", err)
		}
		fmt.Printf("%s acked seat %s through seq %d\n", ui.RenderAccent("*"), seat, upto)
		return nil
	},
}

func openNotifyOutbox() (*notify.Outbox, error) {
	dir := beads.FindBeadsDir()
	if dir == "" {
		return nil, HandleErrorWithHint("no .beads directory found", "run from a beads workspace")
	}
	o, err := notify.Open(dir)
	if err != nil {
		return nil, HandleError("%v", err)
	}
	return o, nil
}

func runNotifyExec(command string, rec notify.Record) error {
	payload, _ := json.Marshal(rec)
	prompt := fmt.Sprintf("BEADS NOTIFY (%s). Bead %s fired for seat %s: %s",
		rec.Kind, rec.IssueID, rec.Seat, rec.Title)
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("BD_NOTIFY_SEQ=%d", rec.Seq),
		"BD_NOTIFY_SEAT="+rec.Seat,
		"BD_NOTIFY_ID="+rec.IssueID,
		"BD_NOTIFY_KIND="+string(rec.Kind),
		"BD_NOTIFY_TITLE="+rec.Title,
		"BD_NOTIFY_PROMPT="+prompt,
		"BD_NOTIFY_JSON="+string(payload),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// enqueueDueSweepNotifies writes seat-addressed outbox rows for a sweep report.
// Best-effort: a notify failure must not fail the sweep clock.
func enqueueDueSweepNotifies(report dueSweepReport) {
	dir := beads.FindBeadsDir()
	if dir == "" {
		return
	}
	o, err := notify.Open(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "notify: open outbox: %v\n", err)
		return
	}
	// Prefer by_assignee; fall back to flat due_ids as unassigned.
	seats := report.ByAssignee
	if len(seats) == 0 && len(report.DueIDs) > 0 {
		seats = []dueSweepSeat{{Assignee: notify.UnassignedSeat, IDs: report.DueIDs}}
	}
	escalated := map[string]bool{}
	for _, id := range report.EscalatedIDs {
		escalated[id] = true
	}
	var n int
	for _, seat := range seats {
		for _, id := range seat.IDs {
			kind := notify.KindDue
			if escalated[id] {
				kind = notify.KindEscalate
			}
			if _, err := o.Enqueue(seat.Assignee, id, kind, ""); err != nil {
				fmt.Fprintf(os.Stderr, "notify: enqueue %s: %v\n", id, err)
				continue
			}
			n++
		}
	}
	if n > 0 && !jsonOutput {
		fmt.Printf("  notify enqueued %d\n", n)
	}
}

func init() {
	notifyPendingCmd.Flags().String("seat", "", "filter by assignee seat")
	notifyDrainCmd.Flags().String("seat", "", "assignee seat to drain (required)")
	notifyDrainCmd.Flags().Int("limit", 0, "max rows to drain (0 = all pending)")
	notifyDrainCmd.Flags().String("exec", "", "shell command to run per row (see env BD_NOTIFY_*)")
	notifyDrainCmd.Flags().Bool("no-ack", false, "do not mark rows delivered")
	notifyAckCmd.Flags().String("seat", "", "assignee seat (required)")
	notifyAckCmd.Flags().Int64("upto", 0, "ack through this seq (required)")

	notifyCmd.AddCommand(notifyPendingCmd)
	notifyCmd.AddCommand(notifyDrainCmd)
	notifyCmd.AddCommand(notifyAckCmd)
	notifyCmd.AddCommand(notifySeatsCmd)
	rootCmd.AddCommand(notifyCmd)
}
