package issueops

import (
	"fmt"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// DueRequiredKey is the workspace switch that turns a due date from an option
// into a create-time invariant.
//
// It defaults OFF so a workspace that has never heard of it behaves exactly as
// it did before; a fleet that wants every bead to carry a deadline turns it on
// once, in config.yaml, and every create surface answers to it.
const DueRequiredKey = "due.required"

// DueRequiredEnabled reports whether this workspace requires a due date on new
// work.
func DueRequiredEnabled() bool {
	return config.GetBool(DueRequiredKey)
}

// DueRequiredExempt reports whether an issue sits outside the mandatory-due
// invariant because it is not work anyone is meant to finish by a date: an
// audit event, a wisp-plane record, a read-only molecule template, or a row a
// federation adapter authored elsewhere and this database only mirrors.
//
// `bd import` — the backfill path — is exempt at its CALL SITE instead, through
// BatchCreateOptions.SkipDueRequired, because an imported row's exemption is a
// property of the caller restoring it, not of the row itself: the same row
// created by hand still needs a due date.
func DueRequiredExempt(issue *types.Issue) bool {
	if issue == nil {
		return true
	}
	// ponytail: the wisp plane is spelled three ways here because a create may
	// arrive with any one of them set — the flags, or the class marker alone.
	return issue.IssueType == types.TypeEvent ||
		issue.Ephemeral || issue.NoHistory ||
		issue.WispType != "" ||
		issue.StorageClass == types.StorageClassEphemeral ||
		issue.IsTemplate ||
		issue.SourceSystem != ""
}

// ValidateDueRequired refuses a create that would land work with no deadline.
// It is the storage-boundary half of the invariant: every create surface — CLI,
// HTTP, MCP, the public issueops facade — reaches one of its two call sites, so
// the rule cannot be sidestepped by picking a different front door.
func ValidateDueRequired(issue *types.Issue) error {
	if !DueRequiredEnabled() || DueRequiredExempt(issue) || issue.DueAt != nil {
		return nil
	}
	return fmt.Errorf("%w: due date is required for %s %q (pass --due, or set `%s: false` in config.yaml)",
		storage.ErrValidation, issue.IssueType.Normalize(), issue.Title, DueRequiredKey)
}

// DefaultDueRequiredAssignee gives a required-due issue a destination, so
// whatever later reads the deadline has a seat to notify. An explicit assignee
// or owner wins; otherwise the actor creating the work owns it.
func DefaultDueRequiredAssignee(issue *types.Issue, actor string) {
	if !DueRequiredEnabled() || DueRequiredExempt(issue) {
		return
	}
	if issue.Assignee == "" && issue.Owner == "" {
		issue.Assignee = actor
	}
}
