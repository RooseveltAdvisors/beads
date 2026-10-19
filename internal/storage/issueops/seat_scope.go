package issueops

import (
	"context"
	"sync"

	"github.com/steveyegge/beads/internal/seats"
)

type seatScope struct {
	registry seats.Registry
	err      error
}

var seatTransactions sync.Map // transaction -> workspace registry snapshot

// ScopeSeatRegistryTransaction binds validation to the storage transaction,
// including callers which never run the CLI. Enforcement activates only once
// the workspace registry has at least one seat (seats.Strict): a workspace
// that never registered a seat keeps legacy free-text identities, while a
// registered workspace rejects every unknown name and every corrupt registry
// file. Scopes are removed when the transaction is released.
func ScopeSeatRegistryTransaction(tx DBTX, dir string) func() {
	r, err := seats.Load(dir)
	if err == nil && !seats.Strict(r) {
		return func() {}
	}
	seatTransactions.Store(tx, seatScope{r, err})
	return func() { seatTransactions.Delete(tx) }
}

func SeatContext(ctx context.Context, tx DBTX) context.Context {
	if value, ok := seatTransactions.Load(tx); ok {
		scope := value.(seatScope)
		ctx = seats.WithRegistry(ctx, scope.registry)
		if scope.err != nil {
			ctx = seats.WithRegistryError(ctx, scope.err)
		}
	}
	return ctx
}
