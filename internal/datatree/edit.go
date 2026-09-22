package datatree

import (
	"context"
	"errors"
	"strings"

	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Op is one RFC 6241 §7.2 edit operation. The same type also carries the
// <default-operation> values, which add none.
type Op string

// Edit operations.
const (
	OpMerge   Op = "merge"
	OpReplace Op = "replace"
	OpCreate  Op = "create"
	OpDelete  Op = "delete"
	OpRemove  Op = "remove"
	OpNone    Op = "none"
)

// Request is one edit request against a datastore. BasePath is the parent path
// the documents live under ("" for a NETCONF <config>); Default is the
// operation a document without an operation attribute inherits, which is how
// RESTCONF expresses PUT (replace), PATCH (merge) and DELETE.
type Request struct {
	BasePath  string
	Default   Op
	Documents []*Document
}

// Apply edits one datastore with the request's documents. The edits are applied
// to a proposed snapshot of the datastore, the snapshot is validated as a whole
// and only then written back as one atomic batch, so a rejected or interrupted
// edit leaves the datastore untouched. The whole read-modify-write runs inside
// one router transaction, so concurrent edits cannot interleave and lose each
// other's changes.
func Apply(ctx context.Context, r *router.Router, ds store.Datastore, req Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Transaction(ctx, func() error {
		return apply(ctx, r, ds, req)
	})
}

// apply is Apply's body. The caller holds the router's edit transaction, so the
// snapshot cannot change between the read and the write.
func apply(ctx context.Context, r *router.Router, ds store.Datastore, req Request) error {
	results, err := collect(ctx, r, ds)
	if err != nil {
		return err
	}
	original := make(map[string]any, len(results))
	proposed := make(map[string]any, len(results))
	writable := make(map[string]bool, len(results))
	for path, result := range results {
		original[path] = result.Value
		proposed[path] = result.Value
		writable[path] = result.Writable
	}

	schema, err := r.Children(req.BasePath)
	if err != nil {
		return Failed(err)
	}

	e := &editor{router: r, proposed: proposed, writable: writable}
	for _, document := range req.Documents {
		if opErr := e.walk(document, req.BasePath, schema, req.Default); opErr != nil {
			return opErr
		}
	}

	if err := r.Validate(ctx, proposed); err != nil {
		return ValidationError(err)
	}
	return applyDiff(ctx, r, ds, original, proposed)
}

// applyDiff writes the difference between the original and the proposed
// snapshot to the datastore as a single atomic batch, so an error or a
// cancellation cannot leave a half-applied edit behind.
func applyDiff(ctx context.Context, r *router.Router, ds store.Datastore, original, proposed map[string]any) error {
	sets := make(map[string]any)
	deletions := make([]string, 0)
	for path, value := range proposed {
		if old, ok := original[path]; ok && old == value {
			continue
		}
		sets[path] = value
	}
	for path := range original {
		if _, ok := proposed[path]; ok {
			continue
		}
		deletions = append(deletions, path)
	}

	if err := r.Apply(ctx, ds, sets, deletions); err != nil {
		return Failed(err)
	}
	return nil
}

// editor applies a document tree to a proposed snapshot of one datastore.
//
// proposed holds every leaf of the datastore, including the read-only state
// leaves, because the snapshot is written back by difference. writable marks
// the paths configuration data may touch: the read-only leaves are never
// modified, not even by a subtree replace or delete.
type editor struct {
	router   *router.Router
	proposed map[string]any
	writable map[string]bool
}

// walk applies one document of the request. base is the path of its parent and
// schema the parent's schema children; inherited is the operation the parent
// propagates.
func (e *editor) walk(document *Document, base string, schema []router.Node, inherited Op) *Error {
	node := nodeNamed(schema, document.Name)
	if node == nil {
		return UnknownElement(document.Name)
	}

	op, explicit, opErr := parseOperationAttr(document)
	if opErr != nil {
		return opErr
	}
	if !explicit {
		op = inherited
	}

	path := join(base, document.Name)
	switch node.Kind {
	case router.KindLeaf:
		return e.leaf(path, document, *node, op)
	case router.KindContainer:
		return e.container(path, document, op)
	case router.KindList:
		return e.list(base, document, *node, op)
	default:
		return UnknownElement(document.Name)
	}
}

// leaf applies an operation to one leaf.
func (e *editor) leaf(path string, document *Document, node router.Node, op Op) *Error {
	switch op {
	case OpNone:
		return nil
	case OpDelete:
		if !e.writable[path] {
			if _, exists := e.proposed[path]; exists {
				return AccessDenied(path)
			}
			return DataMissing(path)
		}
		e.remove(path)
		return nil
	case OpRemove:
		if _, exists := e.proposed[path]; exists && !e.writable[path] {
			return AccessDenied(path)
		}
		e.remove(path)
		return nil
	}

	value, err := ParseLeaf(node.Leaf, strings.TrimSpace(document.Text))
	if err != nil {
		return InvalidValueAt(path, "%s: %v", path, err)
	}

	// Convert enforces the model's read-only access and the leaf's type
	// without touching the store.
	result, err := e.router.Convert(path, value)
	if err != nil {
		switch {
		case errors.Is(err, router.ErrReadOnly):
			return AccessDenied(path)
		case errors.Is(err, router.ErrNotFound):
			return UnknownElement(document.Name)
		default:
			return InvalidValueAt(path, "%v", err)
		}
	}

	if op == OpCreate && e.writable[path] {
		return DataExists(path)
	}
	e.proposed[path] = result.Value
	e.writable[path] = true
	return nil
}

// container applies an operation to a container and then descends into it.
func (e *editor) container(path string, document *Document, op Op) *Error {
	if opErr := e.prepareSubtree(path, op); opErr != nil {
		return opErr
	}
	if op == OpDelete || op == OpRemove {
		return nil
	}

	children, err := e.router.Children(path)
	if err != nil {
		return UnknownElement(document.Name)
	}

	for _, child := range document.Children {
		if opErr := e.walk(child, path, children, childOperation(op)); opErr != nil {
			return opErr
		}
	}
	return nil
}

// list applies an operation to one list entry and then descends into it. base
// is the path of the list container; the entry is addressed by its key leaf,
// which the document must carry.
func (e *editor) list(base string, document *Document, node router.Node, op Op) *Error {
	key, opErr := listKey(document, node.Key)
	if opErr != nil {
		return opErr
	}
	path := join(base, document.Name) + "[" + node.Key + "=" + key + "]"

	if opErr := e.prepareSubtree(path, op); opErr != nil {
		return opErr
	}
	if op == OpDelete || op == OpRemove {
		return nil
	}

	children, err := e.router.Children(path)
	if err != nil {
		return UnknownElement(document.Name)
	}

	for _, child := range document.Children {
		if opErr := e.walk(child, path, children, childOperation(op)); opErr != nil {
			return opErr
		}
	}
	return nil
}

// childOperation is the operation inherited by the children of a node. A
// replace or create already handled the node itself, so its children are
// written; none propagates unchanged.
func childOperation(op Op) Op {
	switch op {
	case OpReplace, OpCreate:
		return OpMerge
	default:
		return op
	}
}

// prepareSubtree checks and clears the subtree of a container or list entry for
// the given operation.
func (e *editor) prepareSubtree(path string, op Op) *Error {
	switch op {
	case OpCreate:
		if e.contains(path) {
			return DataExists(path)
		}
	case OpDelete:
		if !e.contains(path) {
			return DataMissing(path)
		}
		e.wipe(path)
	case OpRemove:
		e.wipe(path)
	case OpReplace:
		e.wipe(path)
	}
	return nil
}

// contains reports whether the node at path holds configuration data: a
// writable leaf at path, or any writable leaf below it.
func (e *editor) contains(path string) bool {
	if e.writable[path] {
		return true
	}
	needle := path + "/"
	for candidate := range e.proposed {
		if e.writable[candidate] && strings.HasPrefix(candidate, needle) {
			return true
		}
	}
	return false
}

// wipe removes every writable leaf of the subtree at path, leaving the
// read-only state leaves in place.
func (e *editor) wipe(path string) {
	needle := path + "/"
	for candidate := range e.proposed {
		if e.writable[candidate] && strings.HasPrefix(candidate, needle) {
			e.remove(candidate)
		}
	}
	e.remove(path)
}

// remove drops one path from the proposed snapshot.
func (e *editor) remove(path string) {
	delete(e.proposed, path)
	delete(e.writable, path)
}

// parseOperationAttr reads the operation attribute of a document. The attribute
// is matched by local name, so both operation= and nc:operation= work.
func parseOperationAttr(document *Document) (Op, bool, *Error) {
	text, ok := document.Attr("operation")
	if !ok {
		return "", false, nil
	}
	switch op := Op(text); op {
	case OpMerge, OpReplace, OpCreate, OpDelete, OpRemove:
		return op, true, nil
	default:
		return "", false, InvalidValue("unknown operation %q", text)
	}
}

// listKey returns the key value of a list entry document.
func listKey(document *Document, keyName string) (string, *Error) {
	key := document.Child(keyName)
	if key == nil || strings.TrimSpace(key.Text) == "" {
		return "", MissingElement(keyName + " of " + document.Name)
	}

	value := strings.TrimSpace(key.Text)
	if strings.ContainsAny(value, "/[]") {
		return "", InvalidValue("key %s has an invalid value %q", keyName, value)
	}
	return value, nil
}
