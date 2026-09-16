package main

import "testing"

func TestShouldDeliverNotifySeat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		seat, actor   string
		all, explicit bool
		want          bool
	}{
		{name: "unassigned skipped", seat: "unassigned", actor: "alice", want: false},
		{name: "empty seat skipped", seat: "", actor: "alice", want: false},
		{name: "actor match", seat: "alice", actor: "alice", want: true},
		{name: "actor case-insensitive", seat: "Alice", actor: "alice", want: true},
		{name: "other assignee skipped", seat: "bob", actor: "alice", want: false},
		{name: "no actor skips", seat: "alice", actor: "", want: false},
		{name: "explicit seat delivers assigned", seat: "bob", actor: "alice", explicit: true, want: true},
		{name: "explicit unassigned still skipped", seat: "unassigned", actor: "alice", explicit: true, want: false},
		{name: "all delivers assigned", seat: "bob", actor: "alice", all: true, want: true},
		{name: "all still skips unassigned", seat: "unassigned", actor: "alice", all: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldDeliverNotifySeat(tc.seat, tc.actor, tc.all, tc.explicit)
			if got != tc.want {
				t.Fatalf("shouldDeliverNotifySeat(%q, %q, all=%v, explicit=%v) = %v, want %v",
					tc.seat, tc.actor, tc.all, tc.explicit, got, tc.want)
			}
		})
	}
}
