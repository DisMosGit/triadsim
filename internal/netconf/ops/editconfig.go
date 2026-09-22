package ops

import (
	"context"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/store"
)

// EditConfig implements the <edit-config> operation. The edits are applied to a
// proposed snapshot of the target datastore, the snapshot is validated as a
// whole and only then written back, so a rejected edit leaves the datastore
// untouched. The subtree semantics themselves live in internal/datatree, shared
// with RESTCONF.
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

	documents := make([]*datatree.Document, 0, len(config.Children))
	for _, child := range config.Children {
		documents = append(documents, documentOf(child))
	}

	err := datatree.Apply(ctx, deps.Router, target, datatree.Request{
		Default:   defaultOperation,
		Documents: documents,
	})
	if err != nil {
		return fromDataTree(err)
	}
	return nil
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
func parseDefaultOperation(operationElement *Element) (datatree.Op, *Error) {
	element := operationElement.Child("default-operation")
	if element == nil {
		return datatree.OpMerge, nil
	}
	switch op := datatree.Op(element.TrimmedText()); op {
	case datatree.OpMerge, datatree.OpReplace, datatree.OpNone:
		return op, nil
	default:
		return "", InvalidValue("unknown default-operation %q", element.TrimmedText())
	}
}
