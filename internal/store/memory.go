package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Validator checks a candidate snapshot before Commit applies it to running.
//
// It receives a copy of the candidate's flat path -> leaf map, so it cannot
// mutate the store. A non-nil error aborts the commit and leaves every
// datastore untouched; Commit wraps it with ErrValidation.
//
// Commit holds the store lock while calling the Validator. The snapshot is
// passed precisely so the Validator never has to call back into the same
// Memory, which would deadlock.
type Validator func(ctx context.Context, values map[string]any) error

// Options configures a Memory store.
type Options struct {
	// StartupFile is the JSON path Commit persists the startup datastore to.
	// An empty path disables file persistence; the in-memory startup datastore
	// is still updated by Commit.
	StartupFile string

	// Validator, when non-nil, checks the candidate before Commit applies it.
	// A nil Validator accepts every candidate.
	Validator Validator
}

// Memory is the in-memory Store implementation. It is safe for concurrent
// use; every datastore is a flat map from router path to leaf value, so
// container nodes never appear as entries.
type Memory struct {
	mu          sync.RWMutex
	running     map[string]any
	candidate   map[string]any
	startup     map[string]any
	startupFile string
	validator   Validator
}

// NewMemory returns an empty store whose three datastores are equal. The
// datastores are populated through Set, or from a persisted startup file with
// LoadStartup.
func NewMemory(opts Options) *Memory {
	return &Memory{
		running:     make(map[string]any),
		candidate:   make(map[string]any),
		startup:     make(map[string]any),
		startupFile: opts.StartupFile,
		validator:   opts.Validator,
	}
}

var _ Store = (*Memory)(nil)

// copyValues returns a shallow copy of values. Leaf values are immutable
// primitives, so the copy is fully detached from the original map.
func copyValues(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for p, v := range values {
		out[p] = v
	}
	return out
}

// SetValidator installs the Validator used by Commit. It exists so a validator
// that needs the store, such as the router, can be installed after the store is
// constructed. Call it before the first Commit; it is not safe to call
// concurrently with Commit.
func (m *Memory) SetValidator(v Validator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validator = v
}

// validPath reports whether path is in canonical form: non-empty, no leading
// or trailing slash, no empty segment and no surrounding whitespace.
func validPath(path string) bool {
	if path == "" || strings.TrimSpace(path) != path {
		return false
	}
	if strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	return !strings.Contains(path, "//")
}

// datastore returns the map behind ds. The caller must hold m.mu.
func (m *Memory) datastore(ds Datastore) (map[string]any, error) {
	switch ds {
	case Running:
		return m.running, nil
	case Candidate:
		return m.candidate, nil
	case Startup:
		return m.startup, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownDatastore, ds)
	}
}

// Get returns the value at path in ds, or ErrNotFound.
func (m *Memory) Get(ctx context.Context, ds Datastore, path string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	values, err := m.datastore(ds)
	if err != nil {
		return nil, err
	}
	value, ok := values[path]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	return value, nil
}

// Set stores value at path in ds. The value must be one of the supported leaf
// types and path must be in canonical form.
func (m *Memory) Set(ctx context.Context, ds Datastore, path string, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validPath(path) {
		return fmt.Errorf("%w: %q", ErrInvalidPath, path)
	}
	if _, ok := leafKindOf(value); !ok {
		return fmt.Errorf("%w: %T", ErrInvalidValue, value)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	values, err := m.datastore(ds)
	if err != nil {
		return err
	}
	values[path] = value
	return nil
}

// Delete removes path from ds, or returns ErrNotFound.
func (m *Memory) Delete(ctx context.Context, ds Datastore, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	values, err := m.datastore(ds)
	if err != nil {
		return err
	}
	if _, ok := values[path]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	delete(values, path)
	return nil
}

// List returns every path in ds strictly below prefix, in lexicographic order.
// An empty prefix lists the whole datastore. The prefix itself is never
// included, because the store holds leaves only.
func (m *Memory) List(ctx context.Context, ds Datastore, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	values, err := m.datastore(ds)
	if err != nil {
		return nil, err
	}

	needle := prefix
	if needle != "" {
		needle += "/"
	}
	paths := make([]string, 0, len(values))
	for p := range values {
		if prefix == "" || strings.HasPrefix(p, needle) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// Diff returns the changes between candidate and running.
func (m *Memory) Diff(ctx context.Context) ([]Change, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return diffValues(m.running, m.candidate), nil
}

// Commit validates the candidate, applies it to running and persists the new
// startup configuration.
//
// Candidate is authoritative: running becomes a copy of candidate. The
// startup file is written before running is swapped, so a failed write or a
// rejected candidate leaves every datastore untouched.
func (m *Memory) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.validator != nil {
		if err := m.validator(ctx, copyValues(m.candidate)); err != nil {
			return fmt.Errorf("store: commit: %w: %w", ErrValidation, err)
		}
	}

	next := copyValues(m.candidate)
	if m.startupFile != "" {
		if err := Save(ctx, m.startupFile, next); err != nil {
			return fmt.Errorf("store: commit: %w", err)
		}
	}

	m.running = next
	m.startup = copyValues(next)
	m.candidate = copyValues(next)
	return nil
}

// Rollback discards every candidate change by copying running back into
// candidate.
func (m *Memory) Rollback(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.candidate = copyValues(m.running)
	return nil
}

// Snapshot returns a copy of the running datastore. Leaf values are immutable
// primitives, so the copy is fully detached from the store.
func (m *Memory) Snapshot(ctx context.Context) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return copyValues(m.running), nil
}

// Restore replaces running with values and persists them as startup, leaving
// candidate untouched. It is how a confirmed commit is rolled back.
//
// The startup file is written before running is swapped, so a failed write
// leaves every datastore untouched. A snapshot of running is by definition a
// valid configuration, so no validation is performed.
func (m *Memory) Restore(ctx context.Context, values map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	next := copyValues(values)
	if m.startupFile != "" {
		if err := Save(ctx, m.startupFile, next); err != nil {
			return fmt.Errorf("store: restore: %w", err)
		}
	}

	m.running = next
	m.startup = copyValues(next)
	return nil
}

// LoadStartup loads the persisted startup datastore into running, candidate
// and startup. It is a no-op when persistence is disabled or the file does not
// exist yet, so it can be called unconditionally at boot.
func (m *Memory) LoadStartup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.startupFile == "" {
		return nil
	}

	values, err := Load(ctx, m.startupFile)
	if err != nil {
		return fmt.Errorf("store: load startup: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.running = copyValues(values)
	m.startup = copyValues(values)
	m.candidate = copyValues(values)
	return nil
}
