package ops

import (
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Deps is what the operations need from the server: the router that maps paths
// onto the model, the configuration datastores and the event bus.
type Deps struct {
	// Router resolves paths against the model and validates candidates.
	Router *router.Router
	// Store holds the running, candidate and startup datastores.
	Store store.Store
	// Bus receives the ConfigChanged event of a successful commit. It may be
	// nil, which only disables publication.
	Bus *event.Bus
}
