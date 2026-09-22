package gnmi

import (
	"context"

	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
)

// Set applies replace, update and delete operations to the running datastore.
// The whole request is one data-tree edit, so it either applies completely or
// leaves the datastore untouched.
func (s *service) Set(ctx context.Context, req *gnmi.SetRequest) (*gnmi.SetResponse, error) {
	if len(req.GetUnionReplace()) > 0 {
		return nil, status.Error(codes.Unimplemented,
			"union_replace is not supported; use replace or update")
	}

	prefix, err := routerPath(req.GetPrefix())
	if err != nil {
		return nil, err
	}

	operations := make([]*operation, 0,
		len(req.GetDelete())+len(req.GetReplace())+len(req.GetUpdate()))
	for _, path := range req.GetDelete() {
		op, err := newOperation(s.server.router, prefix, path, datatree.OpDelete, nil)
		if err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}
	for _, update := range req.GetReplace() {
		op, err := newOperation(s.server.router, prefix, update.GetPath(), datatree.OpReplace, update)
		if err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}
	for _, update := range req.GetUpdate() {
		op, err := newOperation(s.server.router, prefix, update.GetPath(), datatree.OpMerge, update)
		if err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}

	response := &gnmi.SetResponse{Prefix: mustPath(prefix)}
	if len(operations) == 0 {
		return response, nil
	}

	documents := make([]*datatree.Document, 0, len(operations))
	for _, op := range operations {
		documents = append(documents, op.document)
	}

	if err := datatree.Apply(ctx, s.server.router, runningDatastore, datatree.Request{
		Default:   datatree.OpMerge,
		Documents: documents,
	}); err != nil {
		return nil, statusError(err)
	}

	if s.server.bus != nil {
		s.server.bus.Publish(event.Event{
			Type:     event.TypeConfigChanged,
			Resource: "device",
			Message:  "gnmi Set",
		})
	}

	for _, op := range operations {
		response.Response = append(response.Response, &gnmi.UpdateResult{
			Path: op.path,
			Op:   op.op,
		})
	}
	return response, nil
}

// operation is one prepared Set operation: the gNMI path for the response, the
// response operation and the document the data-tree edit applies.
type operation struct {
	path     *gnmi.Path
	op       gnmi.UpdateResult_Operation
	document *datatree.Document
}

// newOperation converts one gNMI operation into a data-tree document. A replace
// or update must address a leaf, because the value of a subtree cannot be
// expressed as a scalar; delete also accepts a list entry or a container.
func newOperation(r *router.Router, prefix string, path *gnmi.Path, op datatree.Op, update *gnmi.Update) (*operation, error) {
	target, err := joinPrefix(prefix, path)
	if err != nil {
		return nil, err
	}
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, "the operation addresses the model root")
	}

	text := ""
	if op != datatree.OpDelete {
		text, err = textOf(update.GetVal())
		if err != nil {
			return nil, err
		}
	}

	document, err := documentFor(r, target, op, text)
	if err != nil {
		return nil, err
	}

	converted, err := gnmiPath(target)
	if err != nil {
		return nil, statusError(err)
	}
	return &operation{path: converted, op: resultOperation(op), document: document}, nil
}

// documentFor builds the data-tree document of one operation. Every operation
// is a document chain rooted at the model root — the request carries no single
// base path, because one Set may touch unrelated subtrees — with the operation
// attribute and the value on its deepest node and the list keys as children of
// the elements that carry them.
func documentFor(r *router.Router, target string, op datatree.Op, text string) (*datatree.Document, error) {
	parsed, err := router.Parse(target)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid path %q: %v", target, err)
	}

	last := parsed.Segments[len(parsed.Segments)-1]
	if op != datatree.OpDelete {
		if last.Key != "" {
			return nil, status.Errorf(codes.InvalidArgument,
				"%s is a list entry; replace and update address a leaf", target)
		}
		kind, err := schemaKind(r, parsed.Segments[:len(parsed.Segments)-1], last.Name)
		if err != nil {
			return nil, err
		}
		if kind != router.KindLeaf {
			return nil, status.Errorf(codes.InvalidArgument,
				"%s is a %s; replace and update address one leaf per operation", target, kind)
		}
	}

	var root, current *datatree.Document
	for _, segment := range parsed.Segments {
		node := &datatree.Document{Name: segment.Name}
		if segment.Key != "" {
			node.Children = append(node.Children, &datatree.Document{
				Name:    segment.Key,
				Text:    segment.Value,
				HasText: true,
			})
		}
		switch {
		case root == nil:
			root = node
		default:
			current.Children = append(current.Children, node)
		}
		current = node
	}

	current.Attrs = map[string]string{"operation": string(op)}
	if op != datatree.OpDelete {
		current.Text = text
		current.HasText = true
	}
	return root, nil
}

// resultOperation maps a data-tree operation onto the gNMI response operation.
func resultOperation(op datatree.Op) gnmi.UpdateResult_Operation {
	switch op {
	case datatree.OpReplace:
		return gnmi.UpdateResult_REPLACE
	case datatree.OpDelete:
		return gnmi.UpdateResult_DELETE
	default:
		return gnmi.UpdateResult_UPDATE
	}
}

// mustPath converts a canonical path whose syntax the router already accepted.
func mustPath(path string) *gnmi.Path {
	converted, err := gnmiPath(path)
	if err != nil {
		return &gnmi.Path{}
	}
	return converted
}
