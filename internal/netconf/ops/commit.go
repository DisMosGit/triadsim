package ops

import (
	"context"
	"errors"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Commit implements the <commit> operation: the candidate datastore is
// validated, applied to running and persisted as the startup configuration, and
// a ConfigChanged event is published. The optional <source> is accepted for the
// candidate only.
//
// Confirmed commits (<confirmed/>, <confirm-timeout>, <persist>, <persist-id>)
// are not implemented yet.
func Commit(ctx context.Context, deps Deps, operation *Element) error {
	if opErr := checkCommit(operation); opErr != nil {
		return opErr
	}

	if err := deps.Store.Commit(ctx); err != nil {
		if errors.Is(err, store.ErrValidation) {
			return InvalidValue("%v", err)
		}
		return Failed(err)
	}

	deps.publishConfigChanged()
	return nil
}

// checkCommit rejects the commit parameters the simulator does not implement.
func checkCommit(operation *Element) *Error {
	if source := operation.Child("source"); source != nil {
		datastore, opErr := datastoreElement(source)
		if opErr != nil {
			return opErr
		}
		if datastore != store.Candidate {
			return NotSupported("commit source %s is not supported", datastore)
		}
	}

	for _, name := range []string{"confirmed", "confirm-timeout", "persist", "persist-id"} {
		if operation.Child(name) != nil {
			return NotSupported("commit %s is not supported (confirmed commit arrives in a later phase)", name)
		}
	}
	return nil
}

// publishConfigChanged announces a committed configuration change on the event
// bus. The bus stamps the timestamp from its clock.
func (d Deps) publishConfigChanged() {
	if d.Bus == nil {
		return
	}
	d.Bus.Publish(event.Event{
		Type:     event.TypeConfigChanged,
		Resource: "device",
		Message:  "commit",
	})
}
