package ops

import (
	"context"
	"errors"
	"strings"

	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// operation is one RFC 6241 §7.2 edit operation. The same type also carries the
// <default-operation> values, which add none.
type operation string

const (
	opMerge   operation = "merge"
	opReplace operation = "replace"
	opCreate  operation = "create"
	opDelete  operation = "delete"
	opRemove  operation = "remove"
	opNone    operation = "none"
)

// EditConfig implements the <edit-config> operation. The edits are applied to a
// proposed snapshot of the target datastore, the snapshot is validated as a
// whole and only then written back, so a rejected edit leaves the datastore
// untouched.
func EditConfig(ctx context.Context, deps Deps, operationElement *Element) error {
	target, opErr := targetDatastore(operationElement)
	if opErr != nil {
		return opErr
	}
	if opErr := checkEditOptions(operationElement); opErr != nil {
		return opErr
	}
	defaultOperation, opErr := parseDefaultOperation(operationElement)
	if opErr != nil {
		return opErr
	}

	config := operationElement.Child("config")
	if config == nil {
		return MissingElement("config")
	}

	results, err := collectValues(ctx, deps, target)
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

	schema, err := deps.Router.Children("")
	if err != nil {
		return Failed(err)
	}

	w := &editWalker{deps: deps, proposed: proposed, writable: writable}
	for _, child := range config.Children {
		if opErr := w.walk(child, "", schema, defaultOperation); opErr != nil {
			return opErr
		}
	}

	if err := deps.Router.Validate(ctx, proposed); err != nil {
		return InvalidValue("%v", err)
	}
	return apply(ctx, deps, target, original, proposed)
}

// targetDatastore maps the <target> element to a writable datastore. The
// simulator exposes running (writable-running) and candidate; startup is written
// by commit only.
func targetDatastore(operationElement *Element) (store.Datastore, *Error) {
	target := operationElement.Child("target")
	if target == nil {
		return "", MissingElement("target")
	}

	datastore, opErr := datastoreElement(target)
	if opErr != nil {
		return "", opErr
	}
	if datastore == store.Startup {
		return "", NotSupported("edit-config target startup is not supported")
	}
	return datastore, nil
}

// checkEditOptions rejects the optional edit-config options the simulator does
// not implement. The RFC 6241 defaults (test-then-set, stop-on-error) are
// accepted.
func checkEditOptions(operationElement *Element) *Error {
	if element := operationElement.Child("test-option"); element != nil {
		switch text := element.TrimmedText(); text {
		case "", "test-then-set", "set":
		case "test-only":
			return NotSupported("test-option test-only is not supported")
		default:
			return InvalidValue("unknown test-option %q", text)
		}
	}

	if element := operationElement.Child("error-option"); element != nil {
		switch text := element.TrimmedText(); text {
		case "", "stop-on-error":
		case "continue-on-error", "rollback-on-error":
			return NotSupported("error-option %s is not supported", text)
		default:
			return InvalidValue("unknown error-option %q", text)
		}
	}
	return nil
}

// parseDefaultOperation reads <default-operation>; merge is the default.
func parseDefaultOperation(operationElement *Element) (operation, *Error) {
	element := operationElement.Child("default-operation")
	if element == nil {
		return opMerge, nil
	}
	switch op := operation(element.TrimmedText()); op {
	case opMerge, opReplace, opNone:
		return op, nil
	default:
		return "", InvalidValue("unknown default-operation %q", element.TrimmedText())
	}
}

// apply writes the difference between the original and the proposed snapshot to
// the datastore.
func apply(ctx context.Context, deps Deps, ds store.Datastore, original, proposed map[string]any) error {
	for path, value := range proposed {
		if old, ok := original[path]; ok && old == value {
			continue
		}
		if _, err := deps.Router.Set(ctx, ds, path, value); err != nil {
			return Failed(err)
		}
	}
	for path := range original {
		if _, ok := proposed[path]; ok {
			continue
		}
		if err := deps.Router.Delete(ctx, ds, path); err != nil {
			return Failed(err)
		}
	}
	return nil
}

// editWalker applies a <config> tree to a proposed snapshot of one datastore.
//
// proposed holds every leaf of the datastore, including the read-only state
// leaves, because the snapshot is written back by difference. writable marks
// the paths configuration data may touch: the read-only leaves are never
// modified, not even by a subtree replace or delete.
type editWalker struct {
	deps     Deps
	proposed map[string]any
	writable map[string]bool
}

// walk applies one element of the config tree. base is the path of its parent
// and schema the parent's schema children; inherited is the operation the
// parent propagates.
func (w *editWalker) walk(element *Element, base string, schema []router.Node, inherited operation) *Error {
	node := nodeNamed(schema, element.Name)
	if node == nil {
		return UnknownElement(element.Name)
	}

	op, explicit, opErr := parseOperationAttr(element)
	if opErr != nil {
		return opErr
	}
	if !explicit {
		op = inherited
	}

	path := join(base, element.Name)
	switch node.Kind {
	case router.KindLeaf:
		return w.leaf(path, element, *node, op)
	case router.KindContainer:
		return w.container(path, element, op)
	case router.KindList:
		return w.list(base, element, *node, op)
	default:
		return UnknownElement(element.Name)
	}
}

// leaf applies an operation to one leaf.
func (w *editWalker) leaf(path string, element *Element, node router.Node, op operation) *Error {
	switch op {
	case opNone:
		return nil
	case opDelete:
		if !w.writable[path] {
			if _, exists := w.proposed[path]; exists {
				return AccessDenied(path)
			}
			return DataMissing(path)
		}
		w.remove(path)
		return nil
	case opRemove:
		if _, exists := w.proposed[path]; exists && !w.writable[path] {
			return AccessDenied(path)
		}
		w.remove(path)
		return nil
	}

	value, err := parseLeaf(node.Leaf, element.TrimmedText())
	if err != nil {
		return newPathError(TypeProtocol, TagInvalidValue, path, "%s: %v", path, err)
	}

	// Convert enforces the model's read-only access and the leaf's type
	// without touching the store.
	result, err := w.deps.Router.Convert(path, value)
	if err != nil {
		switch {
		case errors.Is(err, router.ErrReadOnly):
			return AccessDenied(path)
		case errors.Is(err, router.ErrNotFound):
			return newPathError(TypeProtocol, TagUnknownElement, path, "unknown element: %s", element.Name)
		default:
			return newPathError(TypeProtocol, TagInvalidValue, path, "%v", err)
		}
	}

	if op == opCreate && w.writable[path] {
		return DataExists(path)
	}
	w.proposed[path] = result.Value
	w.writable[path] = true
	return nil
}

// container applies an operation to a container and then descends into it.
func (w *editWalker) container(path string, element *Element, op operation) *Error {
	if opErr := w.prepareSubtree(path, op); opErr != nil {
		return opErr
	}
	if op == opDelete || op == opRemove {
		return nil
	}

	children, err := w.deps.Router.Children(path)
	if err != nil {
		return UnknownElement(element.Name)
	}

	for _, child := range element.Children {
		if opErr := w.walk(child, path, children, childOperation(op)); opErr != nil {
			return opErr
		}
	}
	return nil
}

// list applies an operation to one list entry and then descends into it. base
// is the path of the list container; the entry is addressed by its key leaf,
// which the config tree must carry.
func (w *editWalker) list(base string, element *Element, node router.Node, op operation) *Error {
	key, opErr := listKey(element, node.Key)
	if opErr != nil {
		return opErr
	}
	path := join(base, element.Name) + "[" + node.Key + "=" + key + "]"

	if opErr := w.prepareSubtree(path, op); opErr != nil {
		return opErr
	}
	if op == opDelete || op == opRemove {
		return nil
	}

	children, err := w.deps.Router.Children(path)
	if err != nil {
		return UnknownElement(element.Name)
	}

	for _, child := range element.Children {
		if opErr := w.walk(child, path, children, childOperation(op)); opErr != nil {
			return opErr
		}
	}
	return nil
}

// childOperation is the operation inherited by the children of a node. A
// replace or create already handled the node itself, so its children are
// written; none propagates unchanged.
func childOperation(op operation) operation {
	switch op {
	case opReplace, opCreate:
		return opMerge
	default:
		return op
	}
}

// prepareSubtree checks and clears the subtree of a container or list entry for
// the given operation.
func (w *editWalker) prepareSubtree(path string, op operation) *Error {
	switch op {
	case opCreate:
		if w.contains(path) {
			return DataExists(path)
		}
	case opDelete:
		if !w.contains(path) {
			return DataMissing(path)
		}
		w.wipe(path)
	case opRemove:
		w.wipe(path)
	case opReplace:
		w.wipe(path)
	}
	return nil
}

// contains reports whether the node at path holds configuration data: a
// writable leaf at path, or any writable leaf below it.
func (w *editWalker) contains(path string) bool {
	if w.writable[path] {
		return true
	}
	needle := path + "/"
	for candidate := range w.proposed {
		if w.writable[candidate] && strings.HasPrefix(candidate, needle) {
			return true
		}
	}
	return false
}

// wipe removes every writable leaf of the subtree at path, leaving the
// read-only state leaves in place.
func (w *editWalker) wipe(path string) {
	needle := path + "/"
	for candidate := range w.proposed {
		if w.writable[candidate] && strings.HasPrefix(candidate, needle) {
			w.remove(candidate)
		}
	}
	w.remove(path)
}

// remove drops one path from the proposed snapshot.
func (w *editWalker) remove(path string) {
	delete(w.proposed, path)
	delete(w.writable, path)
}

// parseOperationAttr reads the operation attribute of an element. The attribute
// is matched by local name, so both operation= and nc:operation= work.
func parseOperationAttr(element *Element) (operation, bool, *Error) {
	text, ok := element.Attr("operation")
	if !ok {
		return "", false, nil
	}
	switch op := operation(text); op {
	case opMerge, opReplace, opCreate, opDelete, opRemove:
		return op, true, nil
	default:
		return "", false, InvalidValue("unknown operation %q", text)
	}
}

// listKey returns the key value of a list entry element.
func listKey(element *Element, keyName string) (string, *Error) {
	key := element.Child(keyName)
	if key == nil || key.TrimmedText() == "" {
		return "", MissingElement(keyName + " of " + element.Name)
	}

	value := key.TrimmedText()
	if strings.ContainsAny(value, "/[]") {
		return "", InvalidValue("key %s has an invalid value %q", keyName, value)
	}
	return value, nil
}
