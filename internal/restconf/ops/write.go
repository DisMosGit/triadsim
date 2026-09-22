package ops

import (
	"context"
	"strings"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Format is a wire encoding of a YANG data document.
type Format string

// The media type formats RESTCONF accepts.
const (
	// FormatJSON is application/yang-data+json.
	FormatJSON Format = "json"
	// FormatXML is application/yang-data+xml.
	FormatXML Format = "xml"
)

// Payload is a decoded request body.
//
// An object or list body becomes one document per resource instance; a scalar
// body (a leaf) becomes Scalar. Name is the resource name with the module
// prefix stripped.
type Payload struct {
	Name      string
	Documents []*datatree.Document
	Scalar    string
	IsScalar  bool
}

// document returns the single document of an object payload, or an error when
// the body carries a list of them.
func (p *Payload) document() (*datatree.Document, *datatree.Error) {
	if p.IsScalar {
		return nil, datatree.InvalidValue("%s expects an object body", p.Name)
	}
	if len(p.Documents) != 1 {
		return nil, datatree.InvalidValue("%s expects exactly one entry in the body", p.Name)
	}
	return p.Documents[0], nil
}

// leafDocument returns the payload as a single leaf document named name.
func (p *Payload) leafDocument(name string) *datatree.Document {
	if p.IsScalar {
		return &datatree.Document{Name: name, Text: p.Scalar, HasText: true}
	}
	if len(p.Documents) == 1 {
		document := p.Documents[0]
		document.Name = name
		return document
	}
	return &datatree.Document{Name: name}
}

// Get reads the resource at path from ds. A missing leaf or list entry is
// reported as data-missing, which the HTTP layer answers with 404.
func Get(ctx context.Context, deps Deps, ds store.Datastore, path string, content datatree.Content) (*datatree.Node, *datatree.Error) {
	node, err := datatree.Read(ctx, deps.Router, ds, datatree.ReadOptions{Prefix: path, Content: content})
	if err != nil {
		return nil, asTreeError(err)
	}
	if node == nil {
		return nil, datatree.DataMissing(path)
	}
	if node.Kind != router.KindContainer && !hasData(node) {
		return nil, datatree.DataMissing(path)
	}
	return node, nil
}

// hasData reports whether a read node carries any data. It is what turns an
// empty leaf or list entry into a 404.
func hasData(node *datatree.Node) bool {
	switch node.Kind {
	case router.KindLeaf:
		return true
	case router.KindList:
		return len(node.Children) > 0
	default:
		return true
	}
}

// Put replaces the addressed resource with the payload.
func Put(ctx context.Context, deps Deps, ds store.Datastore, target Target, payload *Payload) *datatree.Error {
	if target.Collection {
		return datatree.NotSupported("PUT is not supported on the list %s", target.Name)
	}

	document, opErr := documentFor(target, payload)
	if opErr != nil {
		return opErr
	}
	if opErr := target.ensureKey(document); opErr != nil {
		return opErr
	}
	return apply(ctx, deps, ds, target, datatree.OpReplace, []*datatree.Document{document}, "restconf PUT "+target.Path)
}

// Patch merges the payload into the addressed resource. A list collection
// merges every entry the body carries.
func Patch(ctx context.Context, deps Deps, ds store.Datastore, target Target, payload *Payload) *datatree.Error {
	documents, opErr := payloadDocuments(target, payload)
	if opErr != nil {
		return opErr
	}
	for _, document := range documents {
		if opErr := target.ensureKey(document); opErr != nil {
			return opErr
		}
	}
	return apply(ctx, deps, ds, target, datatree.OpMerge, documents, "restconf PATCH "+target.Path)
}

// Post creates the list entries the payload carries.
func Post(ctx context.Context, deps Deps, ds store.Datastore, target Target, payload *Payload) (string, *datatree.Error) {
	if !target.Collection {
		return "", datatree.NotSupported("POST is only supported on a list resource")
	}
	if payload.IsScalar {
		return "", datatree.InvalidValue("%s expects an object body", target.Name)
	}
	for _, document := range payload.Documents {
		if opErr := target.ensureKey(document); opErr != nil {
			return "", opErr
		}
	}
	if opErr := apply(ctx, deps, ds, target, datatree.OpCreate, payload.Documents, "restconf POST "+target.Path); opErr != nil {
		return "", opErr
	}

	// The created entry, as a canonical router path the HTTP layer turns into a
	// Location header.
	if len(payload.Documents) == 1 {
		if key := payload.Documents[0].Child(target.Schema.Key); key != nil {
			return target.Path + "[" + target.Schema.Key + "=" + trimSpace(key.Text) + "]", nil
		}
	}
	return target.Path, nil
}

// Delete removes the addressed resource. A list collection removes every entry
// it holds.
func Delete(ctx context.Context, deps Deps, ds store.Datastore, target Target) *datatree.Error {
	if target.Collection {
		return deleteCollection(ctx, deps, ds, target)
	}

	document := &datatree.Document{Name: target.documentName()}
	if target.Key != "" {
		document.Children = append(document.Children, &datatree.Document{Name: target.Key, Text: target.KeyValue, HasText: true})
	}
	return apply(ctx, deps, ds, target, datatree.OpDelete, []*datatree.Document{document}, "restconf DELETE "+target.Path)
}

// deleteCollection removes every entry of the addressed list.
func deleteCollection(ctx context.Context, deps Deps, ds store.Datastore, target Target) *datatree.Error {
	node, opErr := Get(ctx, deps, ds, target.Path, datatree.ContentAll)
	if opErr != nil {
		return opErr
	}

	documents := make([]*datatree.Document, 0, len(node.Children))
	for _, entry := range node.Children {
		documents = append(documents, &datatree.Document{
			Name:     target.Name,
			Children: []*datatree.Document{{Name: target.Schema.Key, Text: entry.KeyValue, HasText: true}},
		})
	}
	if len(documents) == 0 {
		return datatree.DataMissing(target.Path)
	}
	return apply(ctx, deps, ds, target, datatree.OpDelete, documents, "restconf DELETE "+target.Path)
}

// documentFor builds the single document of a PUT body, converting a scalar
// body into a leaf document.
func documentFor(target Target, payload *Payload) (*datatree.Document, *datatree.Error) {
	if payload.IsScalar {
		if target.Schema.Kind != router.KindLeaf {
			return nil, datatree.InvalidValue("%s expects an object body", target.Name)
		}
		return &datatree.Document{Name: target.Name, Text: payload.Scalar, HasText: true}, nil
	}
	return payload.document()
}

// payloadDocuments builds the documents of a PATCH or POST body.
func payloadDocuments(target Target, payload *Payload) ([]*datatree.Document, *datatree.Error) {
	if payload.IsScalar {
		if target.Schema.Kind != router.KindLeaf {
			return nil, datatree.InvalidValue("%s expects an object body", target.Name)
		}
		return []*datatree.Document{{Name: target.Name, Text: payload.Scalar, HasText: true}}, nil
	}
	if len(payload.Documents) == 0 {
		return nil, datatree.InvalidValue("the request body holds no data")
	}
	return payload.Documents, nil
}

// apply runs the data-tree edit and publishes the configuration change.
func apply(ctx context.Context, deps Deps, ds store.Datastore, target Target, op datatree.Op, documents []*datatree.Document, reason string) *datatree.Error {
	err := datatree.Apply(ctx, deps.Router, ds, datatree.Request{
		BasePath:  target.BasePath,
		Default:   op,
		Documents: documents,
	})
	if err != nil {
		return asTreeError(err)
	}
	deps.publish(reason)
	return nil
}

// asTreeError converts any error into a datatree error.
func asTreeError(err error) *datatree.Error {
	if treeErr, ok := datatree.AsError(err); ok {
		return treeErr
	}
	return datatree.Failed(err)
}

// trimSpace is strings.TrimSpace, kept local so deps.go stays dependency-free.
func trimSpace(value string) string { return strings.TrimSpace(value) }
