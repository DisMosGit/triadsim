package ops

import "context"

// DiscardChanges implements the <discard-changes> operation: every uncommitted
// candidate change is discarded by copying the running configuration back into
// the candidate datastore.
func DiscardChanges(ctx context.Context, deps Deps, _ *Element) error {
	if err := deps.Store.Rollback(ctx); err != nil {
		return Failed(err)
	}
	return nil
}
