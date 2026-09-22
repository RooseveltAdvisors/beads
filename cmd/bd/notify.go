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
// drain delivers only to an exact pin in .beads/notify/pins.json.
var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Seat-addressed notify outbox (beads-owned delivery)",
	Long: `The notify outbox is how beads delivers due/repeat fires and comment
pings to assignee seats.

bd due sweep enqueues one row per fired bead under its assignee seat
(.beads/notify/). bd comment and bd comments add enqueue a comment ping
for the assignee (skipping self-comments and unassigned beads).
bd notify drain delivers that seat only to the exact herdr
target recorded in .beads/notify/pins.json (session + pane id, or agent
session id). Delivery uses the pinned pane's harness follow-up submit
when the harness supports it (so a due fire does not steer mid-turn),
and Enter/steer otherwise. There is no fuzzy search: a missing pin or a
dead pinned target leaves the row queued and marks the seat for parent
escalation.

bd notify resolve is a human diagnostic. It may show a scored lookalike,
but that match is not used for delivery.

Examples:
  bd notify pending
  bd notify resolve --seat wiseman
  bd notify drain --seat wiseman
  bd notify drain                 # current actor only (--actor / BEADS_ACTOR)
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
		if strings.TrimSpace(seat) == "" {
			seat = currentNotifyActor()
			if isUnassignedNotifySeat(seat) {
				if jsonOutput {
					return printJSON([]notify.Record{})
				}
				fmt.Printf("%s no pending notifies\n", ui.RenderAccent("*"))
				return nil
			}
		}
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
	Short: "Human diagnostic: show pin and a non-authoritative fuzzy match",
	Long: `Show the authoritative pin for a seat (from .beads/notify/pins.json)
and, separately, a fuzzy herdr lookalike.

The fuzzy match is diagnostic only. Drain never uses it. Delivery is exact:
pinned target live → deliver there; no pin or dead pin → hold and escalate.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		seat, _ := cmd.Flags().GetString("seat")
		if strings.TrimSpace(seat) == "" {
			return HandleError("notify resolve requires --seat")
		}
		var pin notify.Pin
		var pinned bool
		var pinLive bool
		var pinInst notify.Instance
		o, err := openNotifyOutbox()
		if err == nil {
			pins, perr := o.LoadPins()
			if perr != nil {
				return HandleError("pins: %v", perr)
			}
			pin, pinned = pins[strings.ToLower(strings.TrimSpace(seat))]
			if pinned && !pin.Valid() {
				pinned = false
			}
		}
		d := notify.HerdrDiscoverer{}
		instances, err := d.ListAllInstances()
		if err != nil {
			return HandleError("herdr discover: %v", err)
		}
		if pinned {
			pinInst, pinLive = notify.FindPinnedInstance(pin, instances)
		}
		inst, fuzzy := notify.ResolveSeat(seat, instances)
		if jsonOutput {
			return printJSON(map[string]any{
				"seat":              seat,
				"pin":               pinOrNil(pinned, pin),
				"pin_live":          pinLive,
				"authoritative":     pinned,
				"diagnostic":        instOrNil(fuzzy, inst),
				"diagnostic_note":   "fuzzy ResolveSeat is not used for delivery",
				"instances_scanned": len(instances),
			})
		}
		if pinned {
			live := "not live — drain will hold and escalate"
			if pinLive {
				live = fmt.Sprintf("live %s %s harness=%s status=%s", pinInst.Session, pinInst.PaneID, pinInst.Harness, pinInst.Status)
			}
			fmt.Printf("%s seat %s pin: %s %s (%s)\n",
				ui.RenderAccent("*"), seat, pin.Session, pin.PaneID, live)
		} else {
			fmt.Printf("%s seat %s: no pin in .beads/notify/pins.json (drain will hold and escalate)\n",
				ui.RenderWarn("!"), seat)
		}
		if !fuzzy {
			fmt.Printf("%s diagnostic (not used for delivery): no fuzzy herdr match (%d scanned)\n",
				ui.RenderAccent("*"), len(instances))
			return nil
		}
		fmt.Printf("%s diagnostic (not used for delivery): %s %s (%s) harness=%s status=%s score=%d [%s]\n",
			ui.RenderAccent("*"), inst.Session, inst.PaneID, inst.Title, inst.Harness, inst.Status, inst.Score, inst.MatchReason)
		return nil
	},
}

func pinOrNil(ok bool, pin notify.Pin) any {
	if !ok {
		return nil
	}
	return pin
}

func instOrNil(ok bool, inst notify.Instance) any {
	if !ok {
		return nil
	}
	return inst
}

var notifyDrainCmd = &cobra.Command{
	Use:   "drain",
	Short: "Deliver pending rows for a seat via herdr (or --exec)",
	Long: `Drain pending notify rows for one seat, the current actor, or --all assigned seats.

Default transport is herdr, exact-pin only:
  1. Load .beads/notify/pins.json (seat → session+pane_id or agent_session_id)
  2. Discover live herdr agents
  3. If the pinned target is live, deliver with that pane's harness mode:
     follow_up (pi Option+Enter, cursor/codex Tab) or steer (Enter) as fallback
  4. Ack on success
  Missing pin or dead pin: leave rows queued, mark escalate, never pick a lookalike.

Harness-agnostic: herdr already knows pi/claude/codex/… in each pane.
Drain picks follow-up vs steer from that harness; it does not import firstmate.

Flags:
  --print   print rows only (no herdr, still acks unless --no-ack)
  --exec    override transport with a shell command (BD_NOTIFY_* env)
  --no-ack  leave rows pending after delivery attempt
  --all     drain every assigned seat that has pending rows
Unassigned beads are never notified. Without --seat/--all, only the current
actor (--actor / BEADS_ACTOR) is drained so BEADS NOTIFY is not broadcast.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		seat, _ := cmd.Flags().GetString("seat")
		all, _ := cmd.Flags().GetBool("all")
		limit, _ := cmd.Flags().GetInt("limit")
		execCmd, _ := cmd.Flags().GetString("exec")
		printOnly, _ := cmd.Flags().GetBool("print")
		noAck, _ := cmd.Flags().GetBool("no-ack")

		explicitSeat := strings.TrimSpace(seat) != ""
		actor := currentNotifyActor()
		if !all && !explicitSeat {
			if isUnassignedNotifySeat(actor) {
				return HandleError("notify drain requires --seat, --all, or a current actor (--actor / BEADS_ACTOR)")
			}
			seat = actor
			explicitSeat = true
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
			pins      map[string]notify.Pin
			discErr   error
			discover  notify.HerdrDiscoverer
		)
		needHerdr := execCmd == "" && !printOnly
		if needHerdr {
			var perr error
			pins, perr = o.LoadPins()
			if perr != nil {
				return HandleError("pins: %v", perr)
			}
			instances, discErr = discover.ListAllInstances()
			if discErr != nil {
				return HandleError("herdr discover: %v", discErr)
			}
		}

		type seatResult struct {
			Seat           string              `json:"seat"`
			Delivered      int                 `json:"delivered"`
			AckedThru      int64               `json:"acked_through,omitempty"`
			Instance       *notify.Instance    `json:"instance,omitempty"`
			DeliveryMode   notify.DeliveryMode `json:"delivery_mode,omitempty"`
			Error          string              `json:"error,omitempty"`
			Escalate       bool                `json:"escalate,omitempty"`
			HoldReason     string              `json:"hold_reason,omitempty"`
			Classification int                 `json:"classification,omitempty"`
		}
		var results []seatResult

		for _, s := range seats {
			if !shouldDeliverNotifySeat(s, actor, all, explicitSeat && !all) {
				continue
			}
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
			if needHerdr {
				dec := notify.DecideDelivery(s, pins, instances)
				if !dec.OK {
					res.Error = holdError(dec)
					res.Escalate = dec.Escalate
					res.HoldReason = dec.Reason
					res.Classification = dec.Classification
					if dec.Escalate {
						if err := o.SetHold(s, notify.Hold{
							Reason:         dec.Reason,
							Session:        dec.Pin.Session,
							PaneID:         dec.Pin.PaneID,
							AgentSessionID: dec.Pin.AgentSessionID,
							Pending:        len(pending),
						}); err != nil {
							return HandleError("hold: %v", err)
						}
					}
					results = append(results, res)
					if !jsonOutput {
						if dec.Escalate {
							fmt.Printf("%s seat %s: %s (%d pending left); escalate\n",
								ui.RenderWarn("!"), s, res.Error, len(pending))
						} else {
							fmt.Printf("%s seat %s: %s (%d pending left); holding until idle\n",
								ui.RenderAccent("*"), s, res.Error, len(pending))
						}
					}
					continue
				}
				inst = dec.Instance
				res.Instance = &inst
				res.DeliveryMode = dec.Mode
				res.Classification = dec.Classification
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
					mode := res.DeliveryMode
					if mode == "" {
						mode = notify.ModeForHarness(inst.Harness)
						res.DeliveryMode = mode
					}
					if err := discover.Deliver(inst, prompt, mode); err != nil {
						res.Error = err.Error()
						if !jsonOutput {
							fmt.Fprintf(os.Stderr, "notify drain: seq %d herdr %s %s: %v\n", rec.Seq, inst.Session, inst.PaneID, err)
						}
						goto finishSeat
					}
					if !jsonOutput {
						fmt.Printf("  %d  → %s %s (%s/%s) %s\n", rec.Seq, inst.Session, inst.PaneID, inst.Harness, mode, rec.IssueID)
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
			if needHerdr && res.Error == "" && delivered > 0 {
				if err := o.ClearHold(s); err != nil {
					return HandleError("clear hold: %v", err)
				}
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
			if err := printJSON(results); err != nil {
				return err
			}
		} else if len(results) == 0 {
			fmt.Printf("%s nothing pending\n", ui.RenderAccent("*"))
		}
		if explicitSeat && !all && len(results) == 1 && results[0].Escalate {
			code := results[0].Classification
			if code == 0 {
				code = 1
			}
			return &exitError{Code: code}
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

// pingCommentAssignee enqueues a pin-addressed notify for the bead's assignee
// after a successful comment. Best-effort: never fails the comment.
func pingCommentAssignee(assignee, issueID, issueTitle, author, text string) {
	seat, ok := notify.CommentAssigneeSeat(assignee, author)
	if !ok {
		return
	}
	o, err := openNotifyOutbox()
	if err != nil {
		return
	}
	title := notify.CommentPingTitle(issueTitle, author, text)
	if _, err := o.Enqueue(seat, issueID, notify.KindComment, title); err != nil {
		fmt.Fprintf(os.Stderr, "notify: enqueue comment ping: %v\n", err)
		return
	}
	tryDrainCommentPing(o, seat)
}

// tryDrainCommentPing delivers pending rows for seat through the exact pin.
// No pin → hold+escalate without herdr discovery. Tests skip herdr entirely.
func tryDrainCommentPing(o *notify.Outbox, seat string) {
	if os.Getenv("BEADS_TEST_MODE") != "" {
		return
	}
	pending, err := o.Pending(seat)
	if err != nil || len(pending) == 0 {
		return
	}
	pins, err := o.LoadPins()
	if err != nil {
		return
	}
	pin, havePin := pins[strings.ToLower(strings.TrimSpace(seat))]
	if !havePin || !pin.Valid() {
		_ = o.SetHold(seat, notify.Hold{Reason: notify.HoldNoPin, Pending: len(pending)})
		return
	}
	discover := notify.HerdrDiscoverer{}
	instances, err := discover.ListAllInstances()
	if err != nil {
		_ = o.SetHold(seat, notify.Hold{
			Reason: notify.HoldDeadPin, Session: pin.Session, PaneID: pin.PaneID,
			AgentSessionID: pin.AgentSessionID, Pending: len(pending),
		})
		return
	}
	dec := notify.DecideDelivery(seat, pins, instances)
	if !dec.OK {
		if dec.Escalate {
			_ = o.SetHold(seat, notify.Hold{
				Reason: dec.Reason, Session: dec.Pin.Session, PaneID: dec.Pin.PaneID,
				AgentSessionID: dec.Pin.AgentSessionID, Pending: len(pending),
			})
		}
		return
	}
	var lastOK int64
	mode := dec.Mode
	if mode == "" {
		mode = notify.ModeForHarness(dec.Instance.Harness)
	}
	for _, rec := range pending {
		prompt := notify.DefaultPrompt(rec)
		if body := beadNotifyBody(rec.IssueID); body != "" {
			prompt = prompt + "\n\n" + body
		}
		if err := discover.Deliver(dec.Instance, prompt, mode); err != nil {
			break
		}
		lastOK = rec.Seq
	}
	if lastOK > 0 {
		_ = o.AckSeat(seat, lastOK)
		_ = o.ClearHold(seat)
	}
}

func holdError(dec notify.Delivery) string {
	switch dec.Reason {
	case notify.HoldDeadPin:
		target := strings.TrimSpace(dec.Pin.Session + " " + dec.Pin.PaneID)
		if dec.Pin.AgentSessionID != "" {
			if target == "" {
				target = dec.Pin.AgentSessionID
			} else {
				target = target + " " + dec.Pin.AgentSessionID
			}
		}
		if target == "" {
			target = "pin"
		}
		return "pinned target " + target + " is not live"
	case notify.HoldBlocked:
		target := strings.TrimSpace(dec.Pin.Session + " " + dec.Pin.PaneID)
		if target == "" {
			target = "pin"
		}
		return "pinned target " + target + " is at an approval dialog (blocked)"
	case notify.HoldUntilIdle:
		return fmt.Sprintf("target %s (%s) is working; holding until idle", dec.Instance.PaneID, dec.Instance.Harness)
	default:
		return "no pin in .beads/notify/pins.json"
	}
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
		if isUnassignedNotifySeat(seat.Assignee) {
			continue
		}
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
		if isUnassignedNotifySeat(sc.Assignee) {
			continue
		}
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
