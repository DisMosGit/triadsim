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
	"strings"

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
	root    *model.Device
	store   store.Store
	schema  *schemaNode
	objects []indexedObject
	byOID   map[string]indexedObject
	byPath  map[string]indexedObject
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
		root:    root,
		store:   st,
		schema:  schema,
		objects: objects,
		byOID:   make(map[string]indexedObject, len(objects)),
		byPath:  make(map[string]indexedObject, len(objects)),
	}
	for _, object := range objects {
		if _, duplicate := r.byOID[object.oid]; duplicate {
			return nil, fmt.Errorf("router: duplicate OID %s", object.oid)
		}
		r.byOID[object.oid] = object
		if object.path != "" {
			if _, duplicate := r.byPath[object.path]; duplicate {
				return nil, fmt.Errorf("router: duplicate path %s", object.path)
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

	sort.Slice(objects, func(i, j int) bool { return CompareOID(objects[i].oid, objects[j].oid) < 0 })
	return objects, nil
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
		return Result{}, err
	}
	return r.result(parsed.String(), value, !resolved.readOnly), nil
}

// Convert resolves path, checks that it is a writable leaf and coerces value
// to the leaf's Go type, without touching the store.
func (r *Router) Convert(path string, value any) (Result, error) {
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
	if resolved.readOnly {
		return Result{}, fmt.Errorf("%w: %s", ErrReadOnly, parsed.String())
	}

	// An exposed object may need its protocol value translated back to the
	// model's type, for example the TruthValue 1/2 of atpc/enabled.
	if object, ok := r.byPath[parsed.String()]; ok && object.decode != nil {
		decoded, err := object.decode(value)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w: %v", parsed.String(), ErrTypeMismatch, err)
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
	return r.store.Delete(ctx, ds, parsed.String())
}

// List returns every leaf strictly below prefix in ds.
func (r *Router) List(ctx context.Context, ds store.Datastore, prefix string) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths, err := r.store.List(ctx, ds, prefix)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(paths))
	for _, path := range paths {
		result, err := r.Get(ctx, ds, path)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
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
func (r *Router) Bindings(ctx context.Context, ds store.Datastore) ([]Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	bindings := make([]Binding, 0, len(r.objects))
	for _, object := range r.objects {
		value := object.constant
		if object.path != "" {
			stored, err := r.store.Get(ctx, ds, object.path)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					continue
				}
				return nil, err
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
	return bindings, nil
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
