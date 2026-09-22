package ops

import (
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Deps is what the operations need from the server: the router that maps paths
// onto the model, the configuration datastores, the event bus and the
// confirmed-commit state.
type Deps struct {
	// Router resolves paths against the model and validates candidates.
	Router *router.Router
	// Store holds the running, candidate and startup datastores.
	Store store.Store
	// Bus receives the ConfigChanged event of a successful commit or of a
	// confirmed-commit rollback. It may be nil, which only disables
	// publication.
	Bus *event.Bus
	// SessionID identifies the NETCONF session the operation runs in. It binds
	// a confirmed commit to the session that issued it (RFC 4741 §8.4.1).
	SessionID uint32
	// Confirmed tracks the confirmed commit in progress. The server sets it;
	// when it is nil, <commit><confirmed/> fails with operation-failed.
	Confirmed *Confirmed
}
