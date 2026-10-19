package notify

import (
	"testing"

	"github.com/steveyegge/beads/internal/seats"
)

func openRegisteredTestOutbox(t *testing.T, dir string) (*Outbox, error) {
	t.Helper()
	o, err := Open(dir)
	if err != nil {
		return nil, err
	}
	r := seats.Registry{}
	for _, id := range []string{"wiseman", "firstmate", "arcs-fm", "alice", "bob", "seat-a", "seat-b", "a", "b"} {
		r[id] = seats.Seat{Aliases: []string{id}, Liveness: seats.Dead}
	}
	if err := seats.Save(o.Dir(), r); err != nil {
		t.Fatal(err)
	}
	return o, nil
}
