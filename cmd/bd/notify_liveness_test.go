package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestShouldSuppressNotifyIssue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		issue  *types.Issue
		labels []string
		want   bool
	}{
		{name: "closed bead", issue: &types.Issue{Status: types.StatusClosed}, want: true},
		{name: "held bead", issue: &types.Issue{Status: types.StatusOpen}, labels: []string{"ops", heldNotifyLabel}, want: true},
		{name: "held label removed", issue: &types.Issue{Status: types.StatusOpen}, labels: []string{"ops"}},
		{name: "live bead", issue: &types.Issue{Status: types.StatusOpen}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSuppressNotifyIssue(tc.issue, tc.labels); got != tc.want {
				t.Fatalf("shouldSuppressNotifyIssue(%+v, %v) = %v, want %v", tc.issue, tc.labels, got, tc.want)
			}
		})
	}
}
