// Package store defines the simulator's configuration datastores.
//
// The datastores are the single source of truth shared by every management
// plane: running holds the active configuration, candidate is the
// work-in-progress copy that NETCONF edits, and startup is the snapshot
// persisted to disk and loaded at boot.
//
// memory.go implements this contract in memory; persist.go writes and reads
// the startup datastore as JSON.
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

// ErrUnknownDatastore is returned when a method is called with a datastore
// that is not Running, Candidate or Startup.
var ErrUnknownDatastore = errors.New("store: unknown datastore")

// ErrInvalidPath is returned by Set when a path is empty or not in canonical
// form (no leading or trailing slash, no empty segment).
var ErrInvalidPath = errors.New("store: invalid path")

// ErrInvalidValue is returned when a value is not one of the supported leaf
// types (bool, int, uint8, uint16, uint32, uint64, float64, string).
var ErrInvalidValue = errors.New("store: unsupported value type")

// ErrValidation is returned by Commit when the configured Validator rejects
// the candidate.
var ErrValidation = errors.New("store: candidate validation failed")

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
//   - Snapshot copies running aside, and Restore puts that copy back. Together
//     they are how NETCONF reverts an unconfirmed confirmed commit
//     (RFC 6241 §8.4.1).
//
// Candidate is a superset of running: implementations must mirror every
// mutation of running into candidate, so candidate always holds running plus
// the pending edits. Commit makes running a copy of candidate, and the
// invariant is what keeps that from reverting configuration written by a plane
// that writes running directly (SNMP SET, an edit targeting running, a domain).
//
// Read-only enforcement (nodes tagged config:"false") belongs to
// internal/router, not to the store. Implementations must be safe for
// concurrent use and must respect ctx cancellation.
type Store interface {
	// Get returns the value at path in ds, or ErrNotFound.
	Get(ctx context.Context, ds Datastore, path string) (any, error)
	// Set stores value at path in ds, creating the path when necessary. A
	// write to Running is mirrored into Candidate.
	Set(ctx context.Context, ds Datastore, path string, value any) error
	// Delete removes path from ds, or returns ErrNotFound. Removing a path
	// from Running also removes it from Candidate.
	Delete(ctx context.Context, ds Datastore, path string) error
	// List returns every path in ds below prefix, in lexicographic order.
	List(ctx context.Context, ds Datastore, prefix string) ([]string, error)
	// Diff returns the changes between candidate and running.
	Diff(ctx context.Context) ([]Change, error)
	// Commit validates candidate, applies it to running and persists startup.
	Commit(ctx context.Context) error
	// Rollback discards candidate changes, restoring it from running.
	Rollback(ctx context.Context) error
	// Snapshot returns a detached copy of the whole running datastore. The
	// caller may keep it while running changes and pass it to Restore.
	Snapshot(ctx context.Context) (map[string]any, error)
	// Restore replaces running with values, persists them as startup and
	// leaves candidate untouched. values must come from Snapshot; it is how a
	// confirmed commit is rolled back (RFC 6241 §8.4.1).
	Restore(ctx context.Context, values map[string]any) error
}
