package router

import (
	"context"
	"fmt"
)

// Validate implements store.Validator: it rebuilds a device from the proposed
// flat path -> leaf snapshot and runs the model's Validate(), so a commit can
// never store a value the model rejects.
//
// Rebuilding rather than assigning onto a copy of the boot template is what
// lets a snapshot carry list instances the template never had: a VLAN created
// through RESTCONF or NETCONF becomes a real entry of the rebuilt Device and is
// validated like any other. The rebuild rejects a path that does not resolve in
// the schema, a non-leaf path and a value that does not fit its node.
//
// It never touches the store, so it is safe to call while the store's commit
// lock is held.
func (r *Router) Validate(ctx context.Context, values map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	target, err := r.deviceFromValues(values)
	if err != nil {
		return fmt.Errorf("router: validate: %w", err)
	}
	if err := target.Validate(); err != nil {
		return fmt.Errorf("router: validate: %w", err)
	}
	return nil
}

// Validate is shaped like a store.Validator (the receiver is bound when the
// router is passed to Memory.SetValidator).
var _ func(*Router, context.Context, map[string]any) error = (*Router).Validate
