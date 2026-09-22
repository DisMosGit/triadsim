package router

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/DisMosGit/triadsim/internal/model"
)

// Validate implements store.Validator: it applies a candidate snapshot to a
// copy of the model template and runs the model's Validate().
//
// It never touches the store, so it is safe to call while the store's commit
// lock is held. A path that does not resolve in the template, or a value that
// does not fit its node, is an error — so a store can never contain a value the
// model cannot represent.
func (r *Router) Validate(ctx context.Context, values map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	target, err := cloneDevice(r.root)
	if err != nil {
		return err
	}

	paths := make([]string, 0, len(values))
	for path := range values {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	root := reflect.ValueOf(target)
	for _, path := range paths {
		parsed, err := Parse(path)
		if err != nil {
			return fmt.Errorf("router: validate %s: %w", path, err)
		}
		resolved, err := resolve(root, parsed.Segments)
		if err != nil {
			return fmt.Errorf("router: validate %s: %w", path, err)
		}
		if !isLeaf(resolved.value) {
			return fmt.Errorf("router: validate %s: %w", path, ErrNotFound)
		}
		if err := assignLeaf(resolved.value, values[path]); err != nil {
			return fmt.Errorf("router: validate %s: %w", path, err)
		}
	}

	if err := target.Validate(); err != nil {
		return fmt.Errorf("router: validate: %w", err)
	}
	return nil
}

// cloneDevice deep-copies a device through its JSON representation. The copy
// keeps every concrete type because the model carries json tags on every field.
func cloneDevice(root *model.Device) (*model.Device, error) {
	data, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("router: clone model: %w", err)
	}
	var clone model.Device
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, fmt.Errorf("router: clone model: %w", err)
	}
	return &clone, nil
}

// Validate is shaped like a store.Validator (the receiver is bound when the
// router is passed to Memory.SetValidator).
var _ func(*Router, context.Context, map[string]any) error = (*Router).Validate
