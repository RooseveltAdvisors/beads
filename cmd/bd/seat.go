package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/notify"
	"github.com/steveyegge/beads/internal/seats"
	"github.com/steveyegge/beads/internal/storage"
)

var seatCmd = &cobra.Command{
	Use: "seat", Short: "Register immutable seat identities, aliases, and exact endpoints",
	Long: `Names are mutable aliases. A seat ID is immutable. Endpoints are exact
lookup records, never parsed from a name or ID. Register an ID once, then
re-register the same ID without --alias after restarting an agent.

The first alias is the display name. A rename changes one seats.json row;
beads, actors, and notify rows keep their original identity keys.`,
}

func registryCommand(use, short string, run func(*cobra.Command, []string, *notify.Outbox, seats.Registry) error) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Annotations: map[string]string{"bd:skip_store": "true"}, RunE: func(cmd *cobra.Command, args []string) error {
		o, err := openNotifyOutbox()
		if err != nil {
			return err
		}
		r, err := o.Registry()
		if err != nil {
			return err
		}
		return run(cmd, args, o, r)
	}}
}

func reconcileSeatRegistry(o *notify.Outbox, deadAfter time.Duration) (seats.Registry, error) {
	r, err := o.Registry()
	if err != nil || len(r) == 0 {
		return r, err
	}
	observedAt := time.Now().UTC()
	live, err := (notify.HerdrDiscoverer{}).ListAllInstances()
	if err != nil {
		return nil, fmt.Errorf("seat reconcile: inventory unavailable (registry unchanged): %w", err)
	}
	return o.Reconcile(live, observedAt, deadAfter)
}

type seatBackfiller interface {
	BackfillSeatIDs(context.Context, seats.Registry, bool) ([]seats.BackfillRow, error)
}

var seatBackfillCmd = &cobra.Command{
	Use: "backfill", Short: "Report every legacy identity and re-key beads and pending notifications",
	Long: `Report every assignee, creator, owner, and actor with its exact seat ID,
including every unresolved string and affected bead. No name creates a seat.
Without --apply this is a preview. Register real seats and their explicit
aliases first. Unknown names remain reported with an empty assignee_id.

Pending notify rows are re-keyed by exact registry lookup. Unknown rows stay
queued unless --apply --flush-unresolved explicitly flushes them; each flushed
row is listed, and the original ledger is preserved as outbox.jsonl.pre-seat-identity.
Re-running the command is idempotent. Registry and outbox updates are serialized.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		apply, _ := cmd.Flags().GetBool("apply")
		flush, _ := cmd.Flags().GetBool("flush-unresolved")
		if apply {
			CheckReadonly("seat backfill")
		}
		o, err := openNotifyOutbox()
		if err != nil {
			return err
		}
		release, err := seats.RegistryLock(o.Dir())
		if err != nil {
			return err
		}
		defer release()
		r, err := o.Registry()
		if err != nil {
			return err
		}
		var backend seatBackfiller
		for s := store; s != nil; {
			if b, ok := s.(seatBackfiller); ok {
				backend = b
				break
			}
			u, ok := s.(storage.Unwrapper)
			if !ok {
				break
			}
			s = u.Unwrap()
		}
		if backend == nil {
			return fmt.Errorf("seat backfill requires direct database access")
		}
		// Preview first so an unknown outbox name cannot cause a partial apply.
		notifications, err := o.Backfill(r, false, flush)
		if err != nil {
			return err
		}
		rows, err := backend.BackfillSeatIDs(rootCtx, r, false)
		if err != nil {
			return err
		}
		if jsonOutput {
			if err := printJSON(map[string]any{"applied": apply, "identities": rows, "notifications": notifications}); err != nil {
				return err
			}
		} else {
			for _, row := range rows {
				fmt.Printf("%s %s %s %q -> %q %s\n", row.Table, row.IssueID, row.Field, row.Name, row.SeatID, row.Action)
			}
			for _, row := range notifications {
				fmt.Printf("notify seq=%d bead=%s %q -> %q %s\n", row.Seq, row.IssueID, row.Name, row.SeatID, row.Action)
			}
		}
		if !apply {
			return nil
		}
		for _, row := range notifications {
			if row.Action == "unresolved-held" {
				return fmt.Errorf("unresolved notify names listed above: use --flush-unresolved to explicitly flush them")
			}
		}
		if _, err := backend.BackfillSeatIDs(rootCtx, r, true); err != nil {
			return err
		}
		_, err = o.Backfill(r, true, flush)
		return err
	},
}

func init() {
	register := registryCommand("register <seat-id>", "Register or rebind a seat to an exact endpoint", func(cmd *cobra.Command, args []string, o *notify.Outbox, r seats.Registry) error {
		CheckReadonly("seat register")
		aliases, _ := cmd.Flags().GetStringSlice("alias")
		session, _ := cmd.Flags().GetString("session")
		pane, _ := cmd.Flags().GetString("pane")
		agent, _ := cmd.Flags().GetString("agent-session")
		if err := seats.Update(o.Dir(), func(current seats.Registry) error {
			r = current
			return r.Register(args[0], aliases, seats.Endpoint{Session: session, PaneID: pane, AgentSessionID: agent}, time.Now().UTC())
		}); err != nil {
			return err
		}
		return printJSON(map[string]any{"seat_id": args[0], "seat": r[args[0]]})
	})
	register.Args = cobra.ExactArgs(1)
	register.Flags().StringSlice("alias", nil, "Explicit aliases (first is display name); only on first registration")
	register.Flags().String("session", "", "Exact herdr endpoint session")
	register.Flags().String("pane", "", "Exact endpoint pane ID")
	register.Flags().String("agent-session", "", "Exact agent session ID for endpoint reconciliation")
	rename := registryCommand("rename <seat-id>", "Change aliases on exactly one seat row", func(cmd *cobra.Command, args []string, o *notify.Outbox, r seats.Registry) error {
		CheckReadonly("seat rename")
		aliases, _ := cmd.Flags().GetStringSlice("alias")
		if err := seats.Update(o.Dir(), func(current seats.Registry) error { r = current; return r.Rename(args[0], aliases, time.Now().UTC()) }); err != nil {
			return err
		}
		return printJSON(r[args[0]])
	})
	rename.Args = cobra.ExactArgs(1)
	rename.Flags().StringSlice("alias", nil, "Replace aliases; first alias is the display name")
	list := registryCommand("list", "List the authoritative seat registry", func(cmd *cobra.Command, args []string, o *notify.Outbox, r seats.Registry) error { return printJSON(r) })
	list.Args = cobra.NoArgs
	reconcile := registryCommand("reconcile", "Observe exact herdr sessions, mark stale/dead seats, and escalate dead endpoints", func(cmd *cobra.Command, args []string, o *notify.Outbox, r seats.Registry) error {
		CheckReadonly("seat reconcile")
		deadAfter, _ := cmd.Flags().GetDuration("dead-after")
		if deadAfter <= 0 {
			return fmt.Errorf("--dead-after must be positive")
		}
		r, err := reconcileSeatRegistry(o, deadAfter)
		if err != nil {
			return err
		}
		return printJSON(r)
	})
	reconcile.Args = cobra.NoArgs
	reconcile.Flags().Duration("dead-after", 5*time.Minute, "Escalate after an endpoint has been continuously missing this long")
	seatBackfillCmd.Flags().Bool("apply", false, "Apply the exact-name backfill")
	seatBackfillCmd.Flags().Bool("flush-unresolved", false, "Explicitly flush unresolved notify rows, retaining a backup and per-row report")
	importPins := registryCommand("import-pins", "One-time pins.json to seats.json migration (idempotent, reports every pin)", func(cmd *cobra.Command, args []string, o *notify.Outbox, r seats.Registry) error {
		CheckReadonly("seat import-pins")
		rows, err := notify.ImportPins(o.Dir(), time.Now().UTC())
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(rows)
		}
		if rows == nil {
			fmt.Printf("no legacy %s to import\n", notify.PinsFile)
			return nil
		}
		for _, row := range rows {
			fmt.Printf("pins %q -> %q %s", row.PinKey, row.SeatID, row.Action)
			if row.Detail != "" {
				fmt.Printf(" (%s)", row.Detail)
			}
			fmt.Println()
		}
		return nil
	})
	importPins.Args = cobra.NoArgs
	unbind := registryCommand("unbind <seat-id>", "Mark a seat endpoint dead while retaining its immutable identity", func(cmd *cobra.Command, args []string, o *notify.Outbox, r seats.Registry) error {
		CheckReadonly("seat unbind")
		return seats.Update(o.Dir(), func(current seats.Registry) error {
			seat, ok := current[args[0]]
			if !ok {
				return fmt.Errorf("unknown seat id %q", args[0])
			}
			seat.Endpoint = seats.Endpoint{}
			seat.Session = ""
			seat.Liveness = seats.Dead
			seat.UpdatedAt = time.Now().UTC()
			current[args[0]] = seat
			return nil
		})
	})
	unbind.Args = cobra.ExactArgs(1)
	seatCmd.AddCommand(register, rename, list, reconcile, unbind, importPins, seatBackfillCmd)
	rootCmd.AddCommand(seatCmd)
}
