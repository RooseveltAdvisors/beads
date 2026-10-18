package main

import (
	"encoding/json"
	"testing"

	"github.com/steveyegge/beads/internal/notify"
	"github.com/steveyegge/beads/internal/types"
)

func TestShouldSuppressNotifyRecord(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rec   notify.Record
		issue *types.Issue
		want  bool
	}{
		{name: "closed bead", issue: &types.Issue{Status: types.StatusClosed}, want: true},
		{name: "closed comment", rec: notify.Record{Kind: notify.KindComment}, issue: &types.Issue{Status: types.StatusClosed}, want: true},
		{name: "captain-held due bead", rec: notify.Record{Kind: notify.KindDue}, issue: &types.Issue{Metadata: json.RawMessage(`{"hold_kind":"captain"}`)}, want: true},
		{name: "captain-held escalation", rec: notify.Record{Kind: notify.KindEscalate}, issue: &types.Issue{Metadata: json.RawMessage(`{"hold_kind":"captain"}`)}, want: true},
		{name: "live due bead", rec: notify.Record{Kind: notify.KindDue}, issue: &types.Issue{Status: types.StatusOpen}},
		{name: "captain-held comment", rec: notify.Record{Kind: notify.KindComment}, issue: &types.Issue{Metadata: json.RawMessage(`{"hold_kind":"captain"}`)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSuppressNotifyRecord(tc.rec, tc.issue); got != tc.want {
				t.Fatalf("shouldSuppressNotifyRecord(%+v, %+v) = %v, want %v", tc.rec, tc.issue, got, tc.want)
			}
		})
	}
}
