// Package store defines the simulator's configuration datastores.
//
// The datastores are the single source of truth shared by every management
// plane: running holds the active configuration, candidate is the
// work-in-progress copy that NETCONF edits, and startup is the snapshot
// persisted to disk and loaded at boot.
//
// Phase 0 freezes this contract only; the in-memory implementation with diff,
// commit and JSON persistence lands in Phase 1.5.
package store

import (
	"context"
	"errors"
)

// Datastore names one of the three configuration datastores.
type Datastore string

const (
	// Running is the active configuration used by the device.
	Running Datastore = "running"
	// Candidate is the work-in-progress copy of running.
	Candidate Datastore = "candidate"
	// Startup is the configuration loaded at boot and persisted on disk.
	Startup Datastore = "startup"
)

// ErrNotFound is returned when a path does not exist in the addressed
// datastore.
var ErrNotFound = errors.New("store: path not found")

// Op classifies a Change reported by Diff.
type Op string

const (
	// OpCreate means the path exists only in candidate.
	OpCreate Op = "create"
	// OpUpdate means the path exists in both datastores with different values.
	OpUpdate Op = "update"
	// OpDelete means the path exists only in running.
	OpDelete Op = "delete"
)

// Change is one difference between the candidate and running datastores.
type Change struct {
	Op   Op     `json:"op"`
	Path string `json:"path"`
	Old  any    `json:"old,omitempty"`
	New  any    `json:"new,omitempty"`
}

// Store reads and writes the configuration datastores. Paths are router paths
// such as interfaces/interface[name=radio0]/radio-link/tx-power.
//
// Get, Set, Delete and List address one datastore explicitly, because NETCONF
// get-config can read candidate, copy-config can target startup, and RESTCONF
// accepts ?datastore=candidate. Diff, Commit and Rollback relate candidate to
// running:
//
//   - Diff reports the candidate changes a commit would apply.
//   - Commit validates candidate, applies it to running and persists startup.
//   - Rollback discards candidate and copies running back into it.
//
// Read-only enforcement (nodes tagged config:"false") belongs to
// internal/router, not to the store. Implementations must be safe for
// concurrent use and must respect ctx cancellation.
type Store interface {
	// Get returns the value at path in ds, or ErrNotFound.
	Get(ctx context.Context, ds Datastore, path string) (any, error)
	// Set stores value at path in ds, creating the path when necessary.
	Set(ctx context.Context, ds Datastore, path string, value any) error
	// Delete removes path from ds, or returns ErrNotFound.
	Delete(ctx context.Context, ds Datastore, path string) error
	// List returns every path in ds below prefix, in lexicographic order.
	List(ctx context.Context, ds Datastore, prefix string) ([]string, error)
	// Diff returns the changes between candidate and running.
	Diff(ctx context.Context) ([]Change, error)
	// Commit validates candidate, applies it to running and persists startup.
	Commit(ctx context.Context) error
	// Rollback discards candidate changes, restoring it from running.
	Rollback(ctx context.Context) error
}
