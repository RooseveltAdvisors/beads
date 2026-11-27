package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	issueops "github.com/steveyegge/beads/issueops"
)

// misrouteOwnerSeat is where filed misroutes dispatch from. THE-FM owns lane
// and fleet dispatch, so it triages the filing and hands identity/registry
// class issues to the beads seat (captain order 2026-11-03).
const misrouteOwnerSeat = "firstmate"

// misrouteLabel makes the class visible in triage and countable in the
// discipline monitor without a schema change.
const misrouteLabel = "misroute"

var misrouteCmd = &cobra.Command{
	Use: "misroute <what-was-misrouted>",

	Short: "File a mis-routed task instead of working it",
	Long: `File a mis-routed task as a bead assigned to the dispatch owner.

Captain rule (2026-11-03): work that arrives at a seat that does not own it
is never silently worked, acked, or re-pointed by hand. The receiving seat
files it here and stops. The filing reaches THE-FM through the notify
pipeline - exact uuid-seat delivery, held until live - which a pane prompt
cannot promise: pane ids move on restart, and a mis-pointed report address
is how lanes have drifted twice in one day.

The filed bead carries priority 1, the misroute label, a 48h due, and
acceptance criteria naming the dispatch contract: the owner routes the work
to the owning seat and fixes the sending lane's report address.

Examples:
  bd misroute "deid-fhir lane milestones arriving at wiseman w1:p13F" \
      --from fm-deidfhir --corr deid-fhir-skill
  bd misroute "Covenant Ads wake pings for a thread I do not own" \
      --from bd-due-trigger --corr fm-mw4xg`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		CheckReadonly("misroute")

		what := strings.TrimSpace(args[0])
		if what == "" {
			return HandleErrorRespectJSON("misroute: describe what was misrouted")
		}
		from, _ := cmd.Flags().GetString("from")
		if strings.TrimSpace(from) == "" {
			return HandleErrorRespectJSON("misroute requires --from: name the sender, lane, or seat the work came from")
		}
		corr, _ := cmd.Flags().GetString("corr")

		if store == nil {
			return HandleErrorWithHint("database not initialized", diagHint())
		}
		ops, err := writeOps(store)
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}
		ctx, err := issueOpsContext(rootCtx)
		if err != nil {
			return HandleErrorRespectJSON("%v", err)
		}

		var desc strings.Builder
		fmt.Fprintf(&desc, "Mis-routed work filed by %s.\n\nFrom: %s\n", currentActor(), from)
		if corr != "" {
			fmt.Fprintf(&desc, "Corr: %s\n", corr)
		}
		fmt.Fprintf(&desc, "What: %s\n", what)
		desc.WriteString("\nFiled per the captain rule (2026-11-03): the receiving seat does not\nwork, ack, or re-point mis-routed tasks. Route this work to the seat that\nowns it and fix the sender's report address at its source.")

		now := time.Now().UTC()
		issue := &types.Issue{
			Title:              "MISROUTE: " + what,
			Description:        desc.String(),
			AcceptanceCriteria: "Work routed to the owning seat (not the filer); sender's report address fixed at source; filer confirmed stood-down.",
			Priority:           1,
			IssueType:          types.TypeTask,
			Assignee:           misrouteOwnerSeat,
			Labels:             []string{misrouteLabel},
			DueAt:              &now,
		}
		due := now.Add(48 * time.Hour)
		issue.DueAt = &due
		issue.DueSource = types.DueSourceExplicit

		result, err := ops.Create(ctx, issueops.CreateRequest{
			Actor: currentActor(),
			Issue: issue,
		})
		if err != nil {
			return HandleErrorRespectJSON("misroute: filing failed: %v", err)
		}
		filed := result.Issue
		if jsonOutput {
			return outputJSON(map[string]any{
				"filed":    filed.ID,
				"assignee": misrouteOwnerSeat,
				"label":    misrouteLabel,
			})
		}
		fmt.Printf("%s Misroute filed as %s - assigned to %s, labeled %s. You are stood-down; do not work or ack the task.\n",
			ui.RenderPass("✓"), formatFeedbackID(filed.ID, filed.Title), misrouteOwnerSeat, misrouteLabel)
		return nil
	},
}

func init() {
	misrouteCmd.Flags().String("from", "", "Sender, lane, or seat the misrouted work came from (required)")
	misrouteCmd.Flags().String("corr", "", "Correlation id of the misrouted thread, if any")
	rootCmd.AddCommand(misrouteCmd)
}
