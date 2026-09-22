package ops

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/store"
)

// commitParams are the parameters of one <commit>.
type commitParams struct {
	// confirmed marks a <commit><confirmed/></commit>.
	confirmed bool
	// timeout is the confirm timeout; it only applies to a confirmed commit.
	timeout time.Duration
}

// Commit implements the <commit> operation: the candidate datastore is
// validated, applied to running and persisted as the startup configuration, and
// a ConfigChanged event is published. The optional <source> is accepted for the
// candidate only.
//
// <confirmed/> turns the commit into a confirmed commit: it is reverted to its
// state before the commit unless a confirming commit arrives within
// <confirm-timeout> seconds. Any successful commit is a confirming commit of a
// confirmed commit in progress (RFC 4741 §8.4.1).
//
// The :confirmed-commit:1.1 parameters <persist> and <persist-id>, and the
// <cancel-commit> operation, are not implemented; the server advertises
// :confirmed-commit:1.0.
func Commit(ctx context.Context, deps Deps, operation *Element) error {
	params, opErr := parseCommit(operation)
	if opErr != nil {
		return opErr
	}

	if params.confirmed {
		return commitConfirmed(ctx, deps, params)
	}

	if err := applyCommit(ctx, deps, "commit"); err != nil {
		return err
	}

	// A plain commit is the confirming commit of a confirmed commit in progress,
	// whatever session it arrives on.
	if deps.Confirmed != nil {
		deps.Confirmed.Confirm()
	}
	return nil
}

// commitConfirmed applies a confirmed commit and arms the rollback timer.
func commitConfirmed(ctx context.Context, deps Deps, params commitParams) error {
	if deps.Confirmed == nil {
		return Failed(errors.New("confirmed commit is not configured"))
	}

	// Capture the configuration to revert to before the commit changes running.
	snapshot, err := deps.Store.Snapshot(ctx)
	if err != nil {
		return Failed(err)
	}

	req := ConfirmedRequest{
		SessionID: deps.SessionID,
		Timeout:   params.timeout,
		Rollback:  deps.rollbackTo(snapshot),
	}
	return deps.Confirmed.Apply(ctx, req, func(ctx context.Context) error {
		return applyCommit(ctx, deps, "confirmed-commit")
	})
}

// applyCommit validates the candidate, applies it to running, persists the
// startup configuration and announces the change with reason.
func applyCommit(ctx context.Context, deps Deps, reason string) error {
	if err := deps.Store.Commit(ctx); err != nil {
		if errors.Is(err, store.ErrValidation) {
			return InvalidValue("%v", err)
		}
		return Failed(err)
	}

	deps.publishConfigChanged(reason)
	return nil
}

// rollbackTo builds the rollback of a confirmed commit: running and startup go
// back to the snapshot taken before the commit, while candidate keeps the
// configuration the client has not confirmed yet.
func (d Deps) rollbackTo(snapshot map[string]any) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := d.Store.Restore(ctx, snapshot); err != nil {
			return err
		}
		d.publishConfigChanged("confirmed-commit rollback")
		return nil
	}
}

// parseCommit reads the <commit> parameters the simulator implements.
func parseCommit(operation *Element) (commitParams, *Error) {
	var params commitParams

	if source := operation.Child("source"); source != nil {
		datastore, opErr := datastoreElement(source)
		if opErr != nil {
			return params, opErr
		}
		if datastore != store.Candidate {
			return params, NotSupported("commit source %s is not supported", datastore)
		}
	}

	// <persist> and <persist-id> belong to :confirmed-commit:1.1, which the
	// server does not advertise.
	for _, name := range []string{"persist", "persist-id"} {
		if operation.Child(name) != nil {
			return params, NotSupported("commit %s is not supported by :confirmed-commit:1.0", name)
		}
	}

	confirmed := operation.Child("confirmed")
	timeout := operation.Child("confirm-timeout")
	if timeout != nil && confirmed == nil {
		return params, MissingElement("confirmed (it is required by confirm-timeout)")
	}
	if confirmed == nil {
		return params, nil
	}

	params.confirmed = true
	params.timeout = DefaultConfirmTimeout
	if timeout != nil {
		seconds, err := strconv.Atoi(timeout.TrimmedText())
		if err != nil || seconds <= 0 {
			return params, InvalidValue("confirm-timeout %q is not a positive number of seconds", timeout.TrimmedText())
		}
		params.timeout = time.Duration(seconds) * time.Second
	}
	return params, nil
}

// publishConfigChanged announces a configuration change on the event bus. The
// bus stamps the timestamp from its clock.
func (d Deps) publishConfigChanged(reason string) {
	if d.Bus == nil {
		return
	}
	d.Bus.Publish(event.Event{
		Type:     event.TypeConfigChanged,
		Resource: "device",
		Message:  reason,
	})
}
