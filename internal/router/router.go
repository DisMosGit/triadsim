// Package router maps between management-plane addresses and the managed
// objects in internal/model.
//
// It owns two mappings — path to model navigation via the path tags
// (a/b[c=d]/e) and OID to path for the SNMP MIB tables — plus the RPC dispatch
// used by every management plane. The store is the single source of truth for
// leaf values: the router resolves a path or OID against a model template to
// learn the node's type and access, then reads or writes the store.
package router

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Router errors.
var (
	// ErrNotFound means the path does not resolve to a node in the model.
	ErrNotFound = errors.New("router: node not found")
	// ErrReadOnly means the node is tagged config:"false".
	ErrReadOnly = errors.New("router: node is read-only")
	// ErrTypeMismatch means the value cannot be assigned to the node's type.
	ErrTypeMismatch = errors.New("router: value type mismatch")
	// ErrBadValue means the value has the node's type but can never be held
	// by it: out of range, or outside the node's value domain. SNMP maps it
	// to RFC 3416 wrongValue, distinct from wrongType.
	ErrBadValue = errors.New("router: value out of domain")
	// ErrUnknownOID means the OID is not exposed by the OID tables.
	ErrUnknownOID = errors.New("router: unknown OID")
)

// indexedObject is one concrete SNMP object derived from the model template.
type indexedObject struct {
	oid      string
	path     string // empty for constant objects such as sysObjectID
	typ      SNMPType
	writable bool
	convert  func(any) any
	decode   func(any) (any, error)
	constant any
}

// OpKind is the kind of a dispatched operation.
type OpKind string

// Router operation kinds.
const (
	OpGet    OpKind = "get"
	OpSet    OpKind = "set"
	OpDelete OpKind = "delete"
	OpList   OpKind = "list"
)

// Op is one router operation.
type Op struct {
	Kind      OpKind
	Datastore store.Datastore
	Path      string
	Value     any
}

// Result is the outcome of a router operation. OID and Type are set when the
// path is exposed over SNMP.
type Result struct {
	Path     string
	OID      string
	Value    any
	Type     SNMPType
	Writable bool
}

// Binding is one concrete SNMP object and its current value. Value is already
// in the type the OID table declares, so enum and unit conversions have been
// applied.
type Binding struct {
	OID      string
	Path     string
	Value    any
	Type     SNMPType
	Writable bool
}

// Router resolves model paths and OIDs against a device template and reads and
// writes leaf values in a store.
type Router struct {
	root   *model.Device
	store  store.Store
	schema *schemaNode
	byOID  map[string]indexedObject
	byPath map[string]indexedObject

	// txMu serializes read-modify-write datastore transactions, so two planes
	// cannot interleave a read with each other's write. It is taken by
	// Transaction and never by Set or Delete, which stay single-store-operation
	// calls.
	txMu sync.Mutex

	// bindMu guards bindCache, the memoized SNMP object index.
	bindMu    sync.Mutex
	bindCache bindingCache
}

// bindingCache memoizes the SNMP object index of one datastore generation.
// The bindings slice is shared with callers, which must not modify it.
type bindingCache struct {
	ds         store.Datastore
	generation uint64
	bindings   []Binding
}

// New builds a router for root and st. It fails when the template cannot be
// indexed, for example when a list has no key field or two objects share an
// OID.
func New(root *model.Device, st store.Store) (*Router, error) {
	if root == nil {
		return nil, errors.New("router: root model is nil")
	}
	if st == nil {
		return nil, errors.New("router: store is nil")
	}

	objects, err := buildObjects(root)
	if err != nil {
		return nil, err
	}
	schema, err := buildSchema(reflect.TypeOf(*root))
	if err != nil {
		return nil, err
	}

	r := &Router{
		root:   root,
		store:  st,
		schema: schema,
		byOID:  make(map[string]indexedObject, len(objects)),
		byPath: make(map[string]indexedObject, len(objects)),
	}
	for _, object := range objects {
		if _, duplicate := r.byOID[object.oid]; duplicate {
			return nil, fmt.Errorf("router: duplicate OID %s", object.oid)
		}
		r.byOID[object.oid] = object
		if object.path != "" {
			// One model leaf may back several MIB objects, for example
			// ifAdminStatus and ifOperStatus of the same interface. The map
			// keeps the richest mapping, so a leaf whose writable MIB object
			// needs a decoded value keeps that decoder.
			existing, duplicate := r.byPath[object.path]
			if duplicate && existing.decode != nil {
				continue
			}
			r.byPath[object.path] = object
		}
	}
	return r, nil
}

// buildObjects applies the OID tables to the model template.
func buildObjects(root *model.Device) ([]indexedObject, error) {
	var objects []indexedObject

	for _, rule := range objectRules {
		if rule.scope != scopeScalar {
			continue
		}
		if err := checkPath(root, rule.suffix); err != nil {
			continue // a template without this scalar simply does not expose it
		}
		objects = append(objects, indexedObject{
			oid:      rule.oid + ".0",
			path:     rule.suffix,
			typ:      rule.typ,
			writable: rule.writable,
			convert:  rule.convert,
			decode:   rule.decode,
		})
	}

	objects = append(objects, indexedObject{oid: sysObjectIDOID, typ: TypeObjectID, constant: EnterpriseOID + ".1"})

	radioExposed := false
	for i := range root.Interfaces {
		iface := root.Interfaces[i]
		instance := i + 1
		base := fmt.Sprintf("interfaces/interface[name=%s]", iface.Name)

		objects = append(objects, indexedObject{
			oid:      instanceOID(ifIndexOID, instance),
			typ:      TypeInteger,
			constant: instance,
		})

		for _, rule := range objectRules {
			switch rule.scope {
			case scopeInterface:
				objects = append(objects, indexedObject{
					oid:      instanceOID(rule.oid, instance),
					path:     base + "/" + rule.suffix,
					typ:      rule.typ,
					writable: rule.writable,
					convert:  rule.convert,
					decode:   rule.decode,
				})
			case scopeRadio:
				if iface.RadioLink == nil || radioExposed {
					continue
				}
				objects = append(objects, indexedObject{
					oid:      rule.oid + ".0",
					path:     base + "/radio-link/" + rule.suffix,
					typ:      rule.typ,
					writable: rule.writable,
					convert:  rule.convert,
					decode:   rule.decode,
				})
			}
		}
		if iface.RadioLink != nil {
			radioExposed = true
		}
	}

	objects = append(objects, tableObjects(root, scopeSTPPort, "stp/state/ports/port[port=", func(port string) string {
		if instance := interfaceIndexOf(root, port); instance > 0 {
			return strconv.Itoa(instance)
		}
		return ""
	})...)
	objects = append(objects, tableObjects(root, scopeMAC, "mac-table/entry[mac-address=", macInstance)...)
	objects = append(objects, tableObjects(root, scopeVLAN, "vlans/vlan[id=", func(id string) string { return id })...)
	objects = append(objects, tableObjects(root, scopeSyncE, "synce/interfaces/interface[name=", func(name string) string {
		if instance := interfaceIndexOf(root, name); instance > 0 {
			return strconv.Itoa(instance)
		}
		return ""
	})...)

	sort.Slice(objects, func(i, j int) bool { return CompareOID(objects[i].oid, objects[j].oid) < 0 })
	return objects, nil
}

// tableObjects applies the rules of one table scope to the instances of a list.
// keyed returns the sub-identifier of one instance, or an empty string when the
// instance has none; prefix is the beginning of the model path of an entry,
// which each key closes with a bracket.
func tableObjects(root *model.Device, kind scope, prefix string, keyed func(key string) string) []indexedObject {
	var objects []indexedObject

	for _, key := range tableKeys(root, kind) {
		index := keyed(key)
		if index == "" {
			continue
		}
		base := prefix + key + "]"
		for _, rule := range objectRules {
			if rule.scope != kind {
				continue
			}
			objects = append(objects, indexedObject{
				oid:      indexOID(rule.oid, index),
				path:     base + "/" + rule.suffix,
				typ:      rule.typ,
				writable: rule.writable,
				convert:  rule.convert,
				decode:   rule.decode,
			})
		}
	}
	return objects
}

// tableKeys returns the list keys of one table scope: bridge port names, MAC
// addresses or VLAN identifiers.
func tableKeys(root *model.Device, kind scope) []string {
	switch kind {
	case scopeSTPPort:
		keys := make([]string, 0, len(root.STP.Ports))
		for _, port := range root.STP.Ports {
			keys = append(keys, port.Port)
		}
		return keys
	case scopeMAC:
		keys := make([]string, 0, len(root.MACTable.Entries))
		for _, entry := range root.MACTable.Entries {
			keys = append(keys, entry.MAC)
		}
		return keys
	case scopeVLAN:
		keys := make([]string, 0, len(root.VLANs))
		for _, vlan := range root.VLANs {
			keys = append(keys, strconv.Itoa(int(vlan.ID)))
		}
		return keys
	case scopeSyncE:
		keys := make([]string, 0, len(root.SyncE.Interfaces))
		for _, iface := range root.SyncE.Interfaces {
			keys = append(keys, iface.Name)
		}
		return keys
	default:
		return nil
	}
}

// checkPath reports whether path resolves to a leaf in root.
func checkPath(root *model.Device, path string) error {
	parsed, err := Parse(path)
	if err != nil {
		return err
	}
	resolved, err := resolve(reflect.ValueOf(root), parsed.Segments)
	if err != nil {
		return err
	}
	if !isLeaf(resolved.value) {
		return fmt.Errorf("%w: %s is not a leaf", ErrNotFound, path)
	}
	return nil
}

// Get reads the value at path from ds.
func (r *Router) Get(ctx context.Context, ds store.Datastore, path string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	parsed, err := Parse(path)
	if err != nil {
		return Result{}, err
	}
	resolved, err := resolve(reflect.ValueOf(r.root), parsed.Segments)
	if err != nil {
		return Result{}, err
	}
	if !isLeaf(resolved.value) {
		return Result{}, fmt.Errorf("%w: %s is not a leaf", ErrNotFound, parsed.String())
	}

	value, err := r.store.Get(ctx, ds, parsed.String())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Result{}, fmt.Errorf("%w: %w", ErrNotFound, err)
		}
		return Result{}, err
	}
	return r.result(parsed.String(), value, !resolved.readOnly), nil
}

// Convert resolves path, checks that it is a writable leaf and coerces value
// to the leaf's Go type, without touching the store.
func (r *Router) Convert(path string, value any) (Result, error) {
	return r.convert(path, value, false)
}

// convert is Convert with the read-only rule lifted for domain state writes.
func (r *Router) convert(path string, value any, allowReadOnly bool) (Result, error) {
	parsed, err := Parse(path)
	if err != nil {
		return Result{}, err
	}
	resolved, err := resolve(reflect.ValueOf(r.root), parsed.Segments)
	if err != nil {
		return Result{}, err
	}
	if !isLeaf(resolved.value) {
		return Result{}, fmt.Errorf("%w: %s is not a leaf", ErrNotFound, parsed.String())
	}
	if resolved.readOnly && !allowReadOnly {
		return Result{}, fmt.Errorf("%w: %s", ErrReadOnly, parsed.String())
	}

	// An exposed object may need its protocol value translated back to the
	// model's type, for example the TruthValue 1/2 of atpc/enabled. A value
	// the decoder rejects has the right type but can never be held, which is
	// ErrBadValue rather than ErrTypeMismatch.
	if object, ok := r.byPath[parsed.String()]; ok && object.decode != nil {
		decoded, err := object.decode(value)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w: %v", parsed.String(), ErrBadValue, err)
		}
		value = decoded
	}

	coerced, err := coerce(value, resolved.value.Type())
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", parsed.String(), err)
	}
	return r.result(parsed.String(), coerced, true), nil
}

// Set coerces value to the leaf's type and stores it at path in ds.
func (r *Router) Set(ctx context.Context, ds store.Datastore, path string, value any) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result, err := r.Convert(path, value)
	if err != nil {
		return Result{}, err
	}
	if err := r.store.Set(ctx, ds, result.Path, result.Value); err != nil {
		return Result{}, err
	}
	return result, nil
}

// SetState writes a leaf tagged config:"false" — counters, learned MAC entries,
// measured radio levels — which only the owning domain may do; management
// planes go through Set, which rejects it.
//
// The write targets running and the store mirrors it into candidate, so a later
// NETCONF commit — which makes running a copy of candidate — cannot drop the
// state a domain just wrote. One write also leaves no window in which a commit
// could observe the two datastores apart.
func (r *Router) SetState(ctx context.Context, path string, value any) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result, err := r.convert(path, value, true)
	if err != nil {
		return Result{}, err
	}
	if err := r.store.Set(ctx, store.Running, result.Path, result.Value); err != nil {
		return Result{}, err
	}
	return result, nil
}

// DeleteState removes a read-only state leaf from running; the store mirrors
// the removal into candidate, so the leaf leaves both datastores. A leaf that
// is already absent is not an error, so deleting a learned entry twice is safe.
func (r *Router) DeleteState(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parsed, err := Parse(path)
	if err != nil {
		return err
	}
	resolved, err := resolve(reflect.ValueOf(r.root), parsed.Segments)
	if err != nil {
		return err
	}
	if !isLeaf(resolved.value) {
		return fmt.Errorf("%w: %s is not a leaf", ErrNotFound, parsed.String())
	}

	if err := r.store.Delete(ctx, store.Running, parsed.String()); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

// Delete removes path from ds.
func (r *Router) Delete(ctx context.Context, ds store.Datastore, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parsed, err := Parse(path)
	if err != nil {
		return err
	}
	resolved, err := resolve(reflect.ValueOf(r.root), parsed.Segments)
	if err != nil {
		return err
	}
	if !isLeaf(resolved.value) {
		return fmt.Errorf("%w: %s is not a leaf", ErrNotFound, parsed.String())
	}
	if err := r.store.Delete(ctx, ds, parsed.String()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrNotFound, err)
		}
		return err
	}
	return nil
}

// Transaction runs fn as one datastore transaction, serialized against every
// other transaction. A read-modify-write edit — the NETCONF and RESTCONF edit
// pipeline in internal/datatree, an SNMP SET — uses it so that two planes
// cannot interleave a read with each other's write; the write phase itself goes
// through Apply, which is atomic.
//
// fn must not start a nested transaction: txMu is not reentrant.
func (r *Router) Transaction(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.txMu.Lock()
	defer r.txMu.Unlock()
	return fn()
}

// Apply converts every value and applies the whole batch to ds atomically: the
// store validates the entire batch before it mutates anything, then applies it
// under one lock, so a rejected or interrupted batch cannot leave a
// half-applied edit behind.
//
// Unlike Set, which checks ctx per leaf, Apply checks ctx once: the caller must
// have built a complete batch before calling it.
func (r *Router) Apply(ctx context.Context, ds store.Datastore, values map[string]any, deletions []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	converted := make(map[string]any, len(values))
	for path, value := range values {
		result, err := r.Convert(path, value)
		if err != nil {
			return err
		}
		converted[result.Path] = result.Value
	}
	return r.store.Apply(ctx, ds, converted, deletions)
}

// List returns every leaf strictly below prefix in ds, in lexicographic
// order. It is the ordered projection of Values.
func (r *Router) List(ctx context.Context, ds store.Datastore, prefix string) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := r.store.Values(ctx, ds)
	if err != nil {
		return nil, err
	}

	needle := ""
	if prefix != "" {
		needle = prefix + "/"
	}
	paths := make([]string, 0, len(values))
	for path := range values {
		if prefix == "" || strings.HasPrefix(path, needle) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	results := make([]Result, 0, len(paths))
	for _, path := range paths {
		result, err := r.describe(path, values[path])
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// Values returns every leaf of ds keyed by canonical path. It is the one
// flattening primitive: the store copies a whole datastore in a single locked
// pass and the router resolves each path against the schema exactly once, so
// consumers — SNMP's request index, the data-tree read engine, the device
// rebuild — share one implementation instead of each doing a List with one
// Get per path.
func (r *Router) Values(ctx context.Context, ds store.Datastore) (map[string]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := r.store.Values(ctx, ds)
	if err != nil {
		return nil, err
	}

	results := make(map[string]Result, len(values))
	for path, value := range values {
		result, err := r.describe(path, value)
		if err != nil {
			return nil, err
		}
		results[path] = result
	}
	return results, nil
}

// describe resolves one stored leaf into its Result without touching the
// store again.
func (r *Router) describe(path string, value any) (Result, error) {
	parsed, err := Parse(path)
	if err != nil {
		return Result{}, err
	}
	resolved, err := resolve(reflect.ValueOf(r.root), parsed.Segments)
	if err != nil {
		return Result{}, err
	}
	if !isLeaf(resolved.value) {
		return Result{}, fmt.Errorf("%w: %s is not a leaf", ErrNotFound, parsed.String())
	}
	return r.result(parsed.String(), value, !resolved.readOnly), nil
}

// IsState reports whether path addresses a leaf tagged config:"false". The
// store uses it to keep volatile leaves out of the persisted startup
// document. An unresolvable path is not state.
func (r *Router) IsState(path string) bool {
	parsed, err := Parse(path)
	if err != nil {
		return false
	}
	resolved, err := resolve(reflect.ValueOf(r.root), parsed.Segments)
	if err != nil {
		return false
	}
	return resolved.readOnly
}

// Dispatch executes one operation.
func (r *Router) Dispatch(ctx context.Context, op Op) (Result, error) {
	switch op.Kind {
	case OpGet:
		return r.Get(ctx, op.Datastore, op.Path)
	case OpSet:
		return r.Set(ctx, op.Datastore, op.Path, op.Value)
	case OpDelete:
		if err := r.Delete(ctx, op.Datastore, op.Path); err != nil {
			return Result{}, err
		}
		return Result{Path: op.Path}, nil
	case OpList:
		results, err := r.List(ctx, op.Datastore, op.Path)
		if err != nil {
			return Result{}, err
		}
		return Result{Path: op.Path, Value: results}, nil
	default:
		return Result{}, fmt.Errorf("%w: unknown op %q", ErrInvalidPath, op.Kind)
	}
}

// Seed writes every leaf of the model template into ds.
func (r *Router) Seed(ctx context.Context, ds store.Datastore) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return walkLeaves(reflect.ValueOf(r.root), nil, false, func(path Path, value reflect.Value, _ bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		leaf, err := leafValue(value)
		if err != nil {
			return fmt.Errorf("%s: %w", path.String(), err)
		}
		return r.store.Set(ctx, ds, path.String(), leaf)
	})
}

// Bindings returns every exposed SNMP object with its current value, sorted by
// OID. Objects whose value is missing from the store are skipped.
//
// The OID table is applied to the device rebuilt from ds rather than to the
// boot template, so tables that grow at runtime — VLANs, the MAC forwarding
// database, STP ports — expose the instances the datastore actually holds.
//
// The rebuild is memoized per datastore generation: one SNMP request consults
// the index several times (the varbind lookups, the SET snapshot, the write
// echo), and any leaf write — state included — advances the generation and
// invalidates the cache. The returned slice is shared; callers must not
// modify it.
func (r *Router) Bindings(ctx context.Context, ds store.Datastore) ([]Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	generation, err := r.store.Generation(ctx, ds)
	if err != nil {
		return nil, err
	}
	r.bindMu.Lock()
	if r.bindCache.ds == ds && r.bindCache.generation == generation {
		bindings := r.bindCache.bindings
		r.bindMu.Unlock()
		return bindings, nil
	}
	r.bindMu.Unlock()

	values, err := r.flatValues(ctx, ds)
	if err != nil {
		return nil, err
	}

	device, err := r.deviceFromValues(values)
	if err != nil {
		return nil, err
	}
	objects, err := buildObjects(device)
	if err != nil {
		return nil, err
	}

	bindings := make([]Binding, 0, len(objects))
	for _, object := range objects {
		value := object.constant
		if object.path != "" {
			stored, ok := values[object.path]
			if !ok {
				continue
			}
			value = stored
		}
		if object.convert != nil {
			value = object.convert(value)
		}
		bindings = append(bindings, Binding{
			OID:      object.oid,
			Path:     object.path,
			Value:    value,
			Type:     object.typ,
			Writable: object.writable,
		})
	}

	r.bindMu.Lock()
	r.bindCache = bindingCache{ds: ds, generation: generation, bindings: bindings}
	r.bindMu.Unlock()
	return bindings, nil
}

// Snapshot rebuilds the managed object from ds. The boot template supplies the
// shape and the defaults of containers that are absent from the datastore, so
// a domain always sees a complete, validated device.
func (r *Router) Snapshot(ctx context.Context, ds store.Datastore) (*model.Device, error) {
	values, err := r.flatValues(ctx, ds)
	if err != nil {
		return nil, err
	}
	return r.deviceFromValues(values)
}

// flatValues reads every stored path of ds into a snapshot map. It is the
// store's bulk read, which copies a whole datastore in one locked pass.
func (r *Router) flatValues(ctx context.Context, ds store.Datastore) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.store.Values(ctx, ds)
}

// PathForOID returns the model path behind an exposed OID. A leading dot, as
// produced by some SNMP decoders, is ignored.
func (r *Router) PathForOID(oid string) (Path, bool) {
	oid = strings.TrimPrefix(oid, ".")
	object, ok := r.byOID[oid]
	if !ok || object.path == "" {
		return Path{}, false
	}
	parsed, err := Parse(object.path)
	if err != nil {
		return Path{}, false
	}
	return parsed, true
}

// OIDForPath returns the OID an exposed model path is reachable at.
func (r *Router) OIDForPath(path string) (string, bool) {
	parsed, err := Parse(path)
	if err != nil {
		return "", false
	}
	object, ok := r.byPath[parsed.String()]
	if !ok {
		return "", false
	}
	return object.oid, true
}

// result builds a Result, filling the OID and SNMP type when the path is
// exposed.
func (r *Router) result(path string, value any, writable bool) Result {
	result := Result{Path: path, Value: value, Writable: writable}
	if object, ok := r.byPath[path]; ok {
		result.OID = object.oid
		result.Type = object.typ
	}
	return result
}
