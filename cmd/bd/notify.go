package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/notify"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/ui"
)

// notifyCmd is the beads-owned delivery surface. Time (due sweep) enqueues;
// drain resolves assignee seats to live herdr agent instances (any harness).
var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Seat-addressed notify outbox (beads-owned delivery)",
	Long: `The notify outbox is how beads delivers due/repeat fires to assignee seats.

bd due sweep enqueues one row per fired bead under its assignee seat
(.beads/notify/). bd notify drain resolves that seat to a live herdr agent
instance (pi, claude, codex, … — whatever herdr detects) and prompts it.

No seat is special-cased. Harness support comes from herdr detection, not
from beads knowing each runtime.

Examples:
  bd notify pending
  bd notify resolve --seat wiseman
  bd notify drain --seat wiseman
  bd notify drain --all
  bd notify drain --seat wiseman --print
  bd notify drain --seat wiseman --exec 'echo "$BD_NOTIFY_ID"'
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

var notifyResolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Show which live herdr agent instance a seat maps to",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		seat, _ := cmd.Flags().GetString("seat")
		if strings.TrimSpace(seat) == "" {
			return HandleError("notify resolve requires --seat")
		}
		d := notify.HerdrDiscoverer{}
		instances, err := d.ListAllInstances()
		if err != nil {
			return HandleError("herdr discover: %v", err)
		}
		inst, ok := notify.ResolveSeat(seat, instances)
		if jsonOutput {
			if !ok {
				return printJSON(map[string]any{"seat": seat, "matched": false, "instances_scanned": len(instances)})
			}
			return printJSON(map[string]any{"seat": seat, "matched": true, "instance": inst, "instances_scanned": len(instances)})
		}
		if !ok {
			fmt.Printf("%s seat %s: no live herdr instance (%d scanned)\n", ui.RenderWarn("!"), seat, len(instances))
			return nil
		}
		fmt.Printf("%s seat %s → %s %s (%s) harness=%s status=%s score=%d [%s]\n",
			ui.RenderAccent("*"), seat, inst.Session, inst.PaneID, inst.Title, inst.Harness, inst.Status, inst.Score, inst.MatchReason)
		return nil
	},
}

var notifyDrainCmd = &cobra.Command{
	Use:   "drain",
	Short: "Deliver pending rows for a seat via herdr (or --exec)",
	Long: `Drain pending notify rows for one seat (or --all seats with pending work).

Default transport is herdr:
  1. Discover live agents across running herdr sessions
  2. Resolve assignee seat → best matching instance (session name, title token, cwd)
  3. herdr agent prompt <pane> with a beads notify prompt
  4. Ack on success

Harness-agnostic: herdr already knows pi/claude/codex/… in each pane.

Flags:
  --print   print rows only (no herdr, still acks unless --no-ack)
  --exec    override transport with a shell command (BD_NOTIFY_* env)
  --no-ack  leave rows pending after delivery attempt
  --all     drain every seat that has pending rows`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		seat, _ := cmd.Flags().GetString("seat")
		all, _ := cmd.Flags().GetBool("all")
		limit, _ := cmd.Flags().GetInt("limit")
		execCmd, _ := cmd.Flags().GetString("exec")
		printOnly, _ := cmd.Flags().GetBool("print")
		noAck, _ := cmd.Flags().GetBool("no-ack")

		if !all && strings.TrimSpace(seat) == "" {
			return HandleError("notify drain requires --seat or --all")
		}

		o, err := openNotifyOutbox()
		if err != nil {
			return err
		}

		var seats []string
		if all {
			seats, err = o.Seats()
			if err != nil {
				return HandleError("%v", err)
			}
		} else {
			seats = []string{seat}
		}

		// Discover once per drain invocation when using herdr.
		var (
			instances []notify.Instance
			discErr   error
			discover  notify.HerdrDiscoverer
		)
		needHerdr := execCmd == "" && !printOnly
		if needHerdr {
			instances, discErr = discover.ListAllInstances()
			if discErr != nil {
				return HandleError("herdr discover: %v", discErr)
			}
		}

		type seatResult struct {
			Seat      string           `json:"seat"`
			Delivered int              `json:"delivered"`
			AckedThru int64            `json:"acked_through,omitempty"`
			Instance  *notify.Instance `json:"instance,omitempty"`
			Error     string           `json:"error,omitempty"`
		}
		var results []seatResult

		for _, s := range seats {
			pending, err := o.Pending(s)
			if err != nil {
				return HandleError("%v", err)
			}
			if limit > 0 && len(pending) > limit {
				pending = pending[:limit]
			}
			if len(pending) == 0 {
				continue
			}

			res := seatResult{Seat: s}
			var inst notify.Instance
			var hasInst bool
			if needHerdr {
				inst, hasInst = notify.ResolveSeat(s, instances)
				if !hasInst {
					res.Error = "no live herdr instance for seat"
					results = append(results, res)
					if !jsonOutput {
						fmt.Printf("%s seat %s: %s (%d pending left)\n", ui.RenderWarn("!"), s, res.Error, len(pending))
					}
					continue
				}
				res.Instance = &inst
			}

			var lastOK int64
			var delivered int
			for _, rec := range pending {
				switch {
				case execCmd != "":
					if err := runNotifyExec(execCmd, rec); err != nil {
						res.Error = err.Error()
						if !jsonOutput {
							fmt.Fprintf(os.Stderr, "notify drain: seq %d %s: %v\n", rec.Seq, rec.IssueID, err)
						}
						goto finishSeat
					}
				case printOnly:
					if !jsonOutput {
						fmt.Printf("  %d  %s  %s  %s\n", rec.Seq, rec.Kind, rec.IssueID, rec.Title)
					}
				default:
					prompt := notify.DefaultPrompt(rec)
					// Enrich prompt with bead body when possible (best-effort).
					if body := beadNotifyBody(rec.IssueID); body != "" {
						prompt = prompt + "\n\n" + body
					}
					if err := discover.Prompt(inst, prompt); err != nil {
						res.Error = err.Error()
						if !jsonOutput {
							fmt.Fprintf(os.Stderr, "notify drain: seq %d herdr %s %s: %v\n", rec.Seq, inst.Session, inst.PaneID, err)
						}
						goto finishSeat
					}
					if !jsonOutput {
						fmt.Printf("  %d  → %s %s (%s) %s\n", rec.Seq, inst.Session, inst.PaneID, inst.Harness, rec.IssueID)
					}
				}
				delivered++
				lastOK = rec.Seq
			}
		finishSeat:
			res.Delivered = delivered
			if !noAck && lastOK > 0 {
				if err := o.AckSeat(s, lastOK); err != nil {
					return HandleError("ack: %v", err)
				}
				res.AckedThru = lastOK
			}
			results = append(results, res)
			if !jsonOutput {
				if res.Error != "" && delivered == 0 {
					// already printed
				} else {
					fmt.Printf("%s seat %s: delivered %d (acked through %d)\n",
						ui.RenderAccent("*"), s, delivered, lastOK)
				}
			}
		}

		if jsonOutput {
			return printJSON(results)
		}
		if len(results) == 0 {
			fmt.Printf("%s nothing pending\n", ui.RenderAccent("*"))
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
	prompt := notify.DefaultPrompt(rec)
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

// beadNotifyBody best-effort loads description for richer prompts.
func beadNotifyBody(id string) string {
	if store == nil || id == "" {
		return ""
	}
	issue, err := store.GetIssue(rootCtx, id)
	if err != nil || issue == nil {
		return ""
	}
	return strings.TrimSpace(issue.Description)
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
			title := ""
			if store != nil {
				if issue, err := store.GetIssue(rootCtx, id); err == nil && issue != nil {
					title = issue.Title
				}
			}
			if _, err := o.Enqueue(seat.Assignee, id, kind, title); err != nil {
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


// enqueueStaleClaimNotifies finds assigned in_progress work quiet longer than
// comment.stale_claim_after and enqueues KindStaleClaim (educational playbook).
// Best-effort; never fails the due clock. Skips ids already due-fired this tick
// and ids with a recent stale-claim row (cooldown = same threshold).
func enqueueStaleClaimNotifies(skipIDs []string) {
	after := issueops.StaleClaimAfter()
	if after <= 0 || store == nil {
		return
	}
	dir := beads.FindBeadsDir()
	if dir == "" {
		return
	}
	o, err := notify.Open(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "notify: open outbox (stale-claim): %v\n", err)
		return
	}
	now := time.Now().UTC()
	stale, err := issueops.FindStaleClaims(rootCtx, store, after, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "notify: stale-claim scan: %v\n", err)
		return
	}
	skip := map[string]bool{}
	for _, id := range skipIDs {
		skip[strings.TrimSpace(id)] = true
	}
	// Also skip anything already pending (any kind) for this issue.
	pending, _ := o.Pending("")
	for _, rec := range pending {
		skip[rec.IssueID] = true
	}
	cooldownSince := now.Add(-after)
	const maxPerSweep = 10 // ponytail: teach without flooding every seat in one tick
	var n int
	for _, sc := range stale {
		if n >= maxPerSweep {
			break
		}
		if skip[sc.ID] {
			continue
		}
		recent, err := o.HasRecent(sc.ID, notify.KindStaleClaim, cooldownSince)
		if err != nil {
			fmt.Fprintf(os.Stderr, "notify: stale-claim recent %s: %v\n", sc.ID, err)
			continue
		}
		if recent {
			continue
		}
		title := sc.Title
		if title == "" {
			title = sc.Reason
		} else {
			title = title + " [" + sc.QuietFor + " quiet]"
		}
		if _, err := o.Enqueue(sc.Assignee, sc.ID, notify.KindStaleClaim, title); err != nil {
			fmt.Fprintf(os.Stderr, "notify: enqueue stale-claim %s: %v\n", sc.ID, err)
			continue
		}
		n++
	}
	if n > 0 && !jsonOutput {
		fmt.Printf("  stale-claim enqueued %d\n", n)
	}
}

func init() {
	notifyPendingCmd.Flags().String("seat", "", "filter by assignee seat")
	notifyResolveCmd.Flags().String("seat", "", "assignee seat (required)")
	notifyDrainCmd.Flags().String("seat", "", "assignee seat to drain")
	notifyDrainCmd.Flags().Bool("all", false, "drain every seat with pending rows")
	notifyDrainCmd.Flags().Int("limit", 0, "max rows per seat (0 = all pending)")
	notifyDrainCmd.Flags().String("exec", "", "override transport shell command (BD_NOTIFY_* env)")
	notifyDrainCmd.Flags().Bool("print", false, "print rows only (no herdr prompt)")
	notifyDrainCmd.Flags().Bool("no-ack", false, "do not mark rows delivered")
	notifyAckCmd.Flags().String("seat", "", "assignee seat (required)")
	notifyAckCmd.Flags().Int64("upto", 0, "ack through this seq (required)")

	notifyCmd.AddCommand(notifyPendingCmd)
	notifyCmd.AddCommand(notifyResolveCmd)
	notifyCmd.AddCommand(notifyDrainCmd)
	notifyCmd.AddCommand(notifyAckCmd)
	notifyCmd.AddCommand(notifySeatsCmd)
	rootCmd.AddCommand(notifyCmd)
}
