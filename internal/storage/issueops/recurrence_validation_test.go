package issueops

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

func TestValidateUpdatedRecurrence(t *testing.T) {
	t.Parallel()
	validRecurrence := json.RawMessage(`{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"America/New_York"}`)
	legacyRepeat := json.RawMessage(`{"repeat":3,"campaign":"jonroosevelt.com"}`)

	tests := []struct {
		name       string
		oldIssue   *types.Issue
		updates    map[string]interface{}
		wantErr    string
		wantNilErr bool
	}{
		{
			name:       "valid recurrence untouched",
			oldIssue:   &types.Issue{Assignee: "jr-voice", Metadata: validRecurrence},
			updates:    map[string]interface{}{"title": "retitled"},
			wantNilErr: true,
		},
		{
			name:       "clearing assignee on a recurring issue is refused",
			oldIssue:   &types.Issue{Assignee: "jr-voice", Metadata: validRecurrence},
			updates:    map[string]interface{}{"assignee": nil},
			wantErr:    "canonical owning agent",
			wantNilErr: false,
		},
		{
			name:       "reassigning a recurring issue to another canonical owner is allowed",
			oldIssue:   &types.Issue{Assignee: "jr-voice", Metadata: validRecurrence},
			updates:    map[string]interface{}{"assignee": "pi-enforcement"},
			wantNilErr: true,
		},
		{
			name:       "introducing an invalid schedule through metadata is refused",
			oldIssue:   &types.Issue{Assignee: "jr-voice", Metadata: json.RawMessage(`{}`)},
			updates:    map[string]interface{}{"metadata": json.RawMessage(`{"repeat":"61 9 * * *","recurrence_start":"2026-09-08","recurrence_tz":"UTC"}`)},
			wantErr:    "invalid recurrence schedule",
			wantNilErr: false,
		},
		{
			name:       "legacy repeat survives unrelated updates",
			oldIssue:   &types.Issue{Assignee: "", Metadata: legacyRepeat},
			updates:    map[string]interface{}{"title": "retitled"},
			wantNilErr: true,
		},
		{
			name:       "legacy repeat survives merge edits to sibling keys",
			oldIssue:   &types.Issue{Assignee: "", Metadata: legacyRepeat},
			updates:    map[string]interface{}{"metadata": json.RawMessage(`{"repeat":3,"campaign":"jonroosevelt.com","note":"x"}`)},
			wantNilErr: true,
		},
		{
			name:       "legacy repeat becomes a real recurrence only when valid",
			oldIssue:   &types.Issue{Assignee: "jr-voice", Metadata: legacyRepeat},
			updates:    map[string]interface{}{"metadata": json.RawMessage(`{"repeat":"daily","recurrence_start":"2026-09-08","recurrence_tz":"UTC"}`)},
			wantNilErr: true,
		},
		{
			name:       "legacy repeat cannot be reshaped into an invalid recurrence",
			oldIssue:   &types.Issue{Assignee: "jr-voice", Metadata: legacyRepeat},
			updates:    map[string]interface{}{"metadata": json.RawMessage(`{"repeat":"weekly"}`)},
			wantErr:    `requires "recurrence_start"`,
			wantNilErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateUpdatedRecurrence(tc.oldIssue, tc.updates)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ValidateUpdatedRecurrence() error = %v, want containing %q", err, tc.wantErr)
				}
				if !strings.Contains(err.Error(), storage.ErrValidation.Error()) {
					t.Fatalf("ValidateUpdatedRecurrence() error not matchable as ErrValidation: %v", err)
				}
				return
			}
			if tc.wantNilErr && err != nil {
				t.Fatalf("ValidateUpdatedRecurrence() error = %v, want nil", err)
			}
		})
	}
}
