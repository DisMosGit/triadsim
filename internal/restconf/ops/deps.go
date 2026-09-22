// Package ops implements the RESTCONF datastore operations: GET, PUT, PATCH,
// POST and DELETE over the data tree, plus the JSON and XML codecs for the
// yang-data media types.
//
// The package is HTTP-agnostic: it returns datatree errors carrying the RFC
// 6241 tag vocabulary, and the parent package maps those onto status codes and
// the ietf-restconf error document.
package ops

import (
	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Deps is what a RESTCONF operation needs: the router that resolves paths
// against the model, the datastores and the event bus.
type Deps struct {
	// Router resolves paths against the model and validates snapshots.
	Router *router.Router
	// Store holds the running, candidate and startup datastores.
	Store store.Store
	// Bus receives ConfigChanged after a successful write. It may be nil, which
	// only disables publication.
	Bus *event.Bus
}

// Target is the resource a write addresses.
type Target struct {
	// Path is the canonical router path of the resource. For a list collection
	// it names the list node without a key.
	Path string
	// BasePath is the path of the resource's parent, where the document is
	// rooted.
	BasePath string
	// Name is the resource name, the last path segment.
	Name string
	// Schema is the schema node of the resource.
	Schema router.Node
	// Key and KeyValue are the list key predicate of the resource. They are
	// empty when the resource is not a list entry.
	Key      string
	KeyValue string
	// Collection is true when the resource is a list node addressed without a
	// key, which is the resource POST creates in.
	Collection bool
}

// publish announces a configuration change on the event bus. The bus stamps
// the timestamp from its clock.
func (d Deps) publish(reason string) {
	if d.Bus == nil {
		return
	}
	d.Bus.Publish(event.Event{Type: event.TypeConfigChanged, Resource: "device", Message: reason})
}

// documentName is the name a payload document must carry to address t.
func (t Target) documentName() string { return t.Name }

// ensureKey makes sure the document carries the key leaf the request path
// names, adding it when the body omits it and rejecting a contradiction.
func (t Target) ensureKey(document *datatree.Document) *datatree.Error {
	if t.Key == "" {
		return nil
	}
	if key := document.Child(t.Key); key != nil {
		if text := trimSpace(key.Text); text != t.KeyValue {
			return datatree.InvalidValueAt(t.Path, "key %s=%s does not match the request path", t.Key, text)
		}
		return nil
	}
	document.Children = append(document.Children, &datatree.Document{Name: t.Key, Text: t.KeyValue, HasText: true})
	return nil
}
