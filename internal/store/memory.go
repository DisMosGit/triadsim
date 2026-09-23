package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DisMosGit/triadsim/internal/clock"
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

	// StateFilter reports whether a path holds state data (a leaf tagged
	// config:"false"). Those leaves are excluded from the persisted startup
	// document and never move ConfigChangedAt. A nil filter treats every leaf
	// as configuration.
	StateFilter func(path string) bool

	// Clock is the time source for ConfigChangedAt. A nil Clock is
	// clock.RealClock.
	Clock clock.Clock
}

// Memory is the in-memory Store implementation. It is safe for concurrent
// use; every datastore is a flat map from router path to leaf value, so
// container nodes never appear as entries.
//
// Candidate is a superset of running: every mutation of running is mirrored
// into candidate under the same lock, so candidate always holds running plus
// the pending edits. Commit relies on that invariant when it makes running a
// copy of candidate.
type Memory struct {
	mu            sync.RWMutex
	running       map[string]any
	candidate     map[string]any
	startup       map[string]any
	startupFile   string
	validator     Validator
	stateFilter   func(path string) bool
	clock         clock.Clock
	generation    map[Datastore]uint64
	configChanged map[Datastore]time.Time
}

// NewMemory returns an empty store whose three datastores are equal. The
// datastores are populated through Set, or from a persisted startup file with
// LoadStartup.
func NewMemory(opts Options) *Memory {
	timeSource := opts.Clock
	if timeSource == nil {
		timeSource = clock.RealClock{}
	}
	return &Memory{
		running:       make(map[string]any),
		candidate:     make(map[string]any),
		startup:       make(map[string]any),
		startupFile:   opts.StartupFile,
		validator:     opts.Validator,
		stateFilter:   opts.StateFilter,
		clock:         timeSource,
		generation:    make(map[Datastore]uint64, 3),
		configChanged: make(map[Datastore]time.Time, 3),
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

// SetStateFilter installs the filter that marks state leaves
// (config:"false"). It exists so the router, which knows the schema, can be
// wired in after the store is constructed. Call it before the first Commit.
func (m *Memory) SetStateFilter(f func(path string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateFilter = f
}

// isState reports whether path holds state data. The caller must hold m.mu.
func (m *Memory) isState(path string) bool {
	return m.stateFilter != nil && m.stateFilter(path)
}

// configOnly returns values without the state leaves, which is what the
// persisted startup document holds: startup is a configuration datastore, and
// a volatile leaf (uptime, a counter, a MAC entry's age) must not reappear
// stale after a restart. The caller must hold m.mu.
func (m *Memory) configOnly(values map[string]any) map[string]any {
	if m.stateFilter == nil {
		return copyValues(values)
	}
	out := make(map[string]any, len(values))
	for p, v := range values {
		if m.isState(p) {
			continue
		}
		out[p] = v
	}
	return out
}

// recordWriteLocked advances the change tracking after paths were written or
// removed in ds. A mutation of Running is mirrored into Candidate, so both
// datastores change. Generation always moves; ConfigChangedAt moves only when
// a configuration leaf is involved. The caller must hold m.mu.
func (m *Memory) recordWriteLocked(ds Datastore, paths ...string) {
	m.bumpLocked(ds, paths...)
	if ds == Running {
		m.bumpLocked(Candidate, paths...)
	}
}

// bumpLocked advances the change tracking of one datastore. The caller must
// hold m.mu.
func (m *Memory) bumpLocked(ds Datastore, paths ...string) {
	m.generation[ds]++
	for _, path := range paths {
		if !m.isState(path) {
			m.configChanged[ds] = m.clock.Now()
			return
		}
	}
}

// recordDiffLocked advances the change tracking of ds after a wholesale
// replacement whose effective changes are diff. The caller must hold m.mu.
func (m *Memory) recordDiffLocked(ds Datastore, diff []Change) {
	paths := make([]string, 0, len(diff))
	for _, change := range diff {
		paths = append(paths, change.Path)
	}
	m.bumpLocked(ds, paths...)
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

// mirror propagates a mutation of Running to Candidate, so candidate always
// holds running plus the pending edits. value is ignored when remove is true.
// Mutations of Candidate and Startup are not mirrored. The caller must hold
// m.mu.
func (m *Memory) mirror(ds Datastore, path string, value any, remove bool) {
	if ds != Running {
		return
	}
	if remove {
		delete(m.candidate, path)
		return
	}
	m.candidate[path] = value
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
//
// A write to Running is mirrored into Candidate under the same lock, because
// candidate must stay a superset of running: Commit makes running a copy of
// candidate, so a running-only write would be silently reverted by the next
// commit. This is the path SNMP SET, a NETCONF or RESTCONF edit targeting
// running, and the L2 domain use. A write to Candidate is a pending edit and
// leaves running untouched.
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
	m.mirror(ds, path, value, false)
	m.recordWriteLocked(ds, path)
	return nil
}

// Delete removes path from ds, or returns ErrNotFound. Removing a path from
// Running also removes it from Candidate, so a later Commit cannot resurrect
// the deleted leaves.
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
	m.mirror(ds, path, nil, true)
	m.recordWriteLocked(ds, path)
	return nil
}

// Apply atomically applies values and deletions to ds. Every path and value is
// validated before anything is written, so a rejected batch leaves ds
// untouched. A deletion that is already absent is not an error, because
// removing the leaves of a subtree is the normal case. A batch targeting
// Running is mirrored into Candidate, like Set and Delete.
func (m *Memory) Apply(ctx context.Context, ds Datastore, values map[string]any, deletions []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for path, value := range values {
		if !validPath(path) {
			return fmt.Errorf("%w: %q", ErrInvalidPath, path)
		}
		if _, ok := leafKindOf(value); !ok {
			return fmt.Errorf("%w: %T", ErrInvalidValue, value)
		}
	}
	for _, path := range deletions {
		if !validPath(path) {
			return fmt.Errorf("%w: %q", ErrInvalidPath, path)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	target, err := m.datastore(ds)
	if err != nil {
		return err
	}
	for path, value := range values {
		target[path] = value
		m.mirror(ds, path, value, false)
	}
	for _, path := range deletions {
		delete(target, path)
		m.mirror(ds, path, nil, true)
	}

	written := make([]string, 0, len(values)+len(deletions))
	for path := range values {
		written = append(written, path)
	}
	written = append(written, deletions...)
	m.recordWriteLocked(ds, written...)
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
// Candidate is authoritative: running becomes a copy of candidate. Because
// every write to running is mirrored into candidate, this cannot revert
// configuration that a plane wrote without using candidate. The startup file
// is written before running is swapped, so a failed write or a rejected
// candidate leaves every datastore untouched. Only configuration leaves are
// persisted: state leaves (config:"false") are volatile and must not reappear
// stale after a restart.
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
	diff := diffValues(m.running, next)
	if m.startupFile != "" {
		if err := Save(ctx, m.startupFile, m.configOnly(next)); err != nil {
			return fmt.Errorf("store: commit: %w", err)
		}
	}

	m.running = next
	m.startup = copyValues(next)
	m.candidate = copyValues(next)
	for _, ds := range []Datastore{Running, Candidate, Startup} {
		m.recordDiffLocked(ds, diff)
	}
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

	diff := diffValues(m.candidate, m.running)
	m.candidate = copyValues(m.running)
	m.recordDiffLocked(Candidate, diff)
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
// valid configuration, so no validation is performed. Only configuration
// leaves are persisted, like in Commit.
func (m *Memory) Restore(ctx context.Context, values map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	next := copyValues(values)
	diff := diffValues(m.running, next)
	if m.startupFile != "" {
		if err := Save(ctx, m.startupFile, m.configOnly(next)); err != nil {
			return fmt.Errorf("store: restore: %w", err)
		}
	}

	m.running = next
	m.startup = copyValues(next)
	m.recordDiffLocked(Running, diff)
	m.recordDiffLocked(Startup, diff)
	return nil
}

// LoadStartup loads the persisted startup datastore into running, candidate
// and startup. It is a no-op when persistence is disabled or the file does not
// exist yet, so it can be called unconditionally at boot.
//
// The loaded leaves are untrusted operator input: state leaves a document
// written by an older version still carries are dropped (they are volatile and
// would reappear stale), and the Validator runs on the result before anything
// is installed, so a hand-edited or corrupted-but-parseable file is rejected
// with a path-level error at the boundary instead of failing later, in the
// middle of domain setup.
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

	// The hooks are installed once, before the first LoadStartup; read them
	// without holding the lock across their calls.
	m.mu.RLock()
	filter, validator := m.stateFilter, m.validator
	m.mu.RUnlock()

	if filter != nil {
		for path := range values {
			if filter(path) {
				delete(values, path)
			}
		}
	}
	if validator != nil {
		if err := validator(ctx, values); err != nil {
			return fmt.Errorf("store: load startup: invalid startup file: %w", err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	diff := diffValues(m.running, values)
	m.running = copyValues(values)
	m.startup = copyValues(values)
	m.candidate = copyValues(values)
	for _, ds := range []Datastore{Running, Candidate, Startup} {
		m.recordDiffLocked(ds, diff)
	}
	return nil
}

// Values returns a detached copy of every leaf of ds in one pass.
func (m *Memory) Values(ctx context.Context, ds Datastore) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	values, err := m.datastore(ds)
	if err != nil {
		return nil, err
	}
	return copyValues(values), nil
}

// Generation returns ds's change counter.
func (m *Memory) Generation(ctx context.Context, ds Datastore) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, err := m.datastore(ds); err != nil {
		return 0, err
	}
	return m.generation[ds], nil
}

// ConfigChangedAt returns the time of ds's last configuration change. State
// writes do not move it; the zero time means no configuration change has been
// recorded yet.
func (m *Memory) ConfigChangedAt(ctx context.Context, ds Datastore) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, err := m.datastore(ds); err != nil {
		return time.Time{}, err
	}
	return m.configChanged[ds], nil
}
