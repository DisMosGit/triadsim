package restconf

import (
	"net/http"
	"strings"

	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// content selects which leaves a read returns, mirroring the ?content= query
// parameter of RFC 8040 §4.8.2.
type content string

// Content selections.
const (
	contentAll    content = "all"
	contentConfig content = "config"
	contentState  content = "nonconfig"
)

// target is one parsed RESTCONF request target: the resource path resolved
// against the router schema plus the datastore and content selection.
type target struct {
	// Path is the canonical router path of the addressed node. For a list
	// collection it names the list node without a key.
	Path string
	// BasePath is the path of the last segment's parent, which is where a
	// write document is rooted.
	BasePath string
	// Name is the name of the last path segment.
	Name string
	// Segment is the canonical last segment, including its key predicate when
	// the target is a list entry.
	Segment router.Segment
	// Node is the schema node of the last segment.
	Node router.Node
	// Collection is true when the last segment names a list without a key,
	// which is the resource a POST creates in.
	Collection bool
	// Datastore is the addressed datastore.
	Datastore store.Datastore
	// Content is the ?content= selection.
	Content content
}

// parseTarget resolves r's URL against the router schema. A malformed path or
// an unknown node is reported as one of the RESTCONF error responses.
func (s *Server) parseTarget(r *http.Request) (*target, *httpError) {
	prefix := BasePath + "/data"
	raw := strings.TrimPrefix(r.URL.Path, prefix)
	if raw == r.URL.Path {
		return nil, notFound(r.URL.Path)
	}

	datastore, httpErr := parseDatastore(r.URL.Query().Get("datastore"))
	if httpErr != nil {
		return nil, httpErr
	}
	selected, httpErr := parseContent(r.URL.Query().Get("content"))
	if httpErr != nil {
		return nil, httpErr
	}

	target := &target{Datastore: datastore, Content: selected}

	raw = strings.Trim(raw, "/")
	if raw == "" {
		return target, nil // the datastore root
	}

	parent := ""
	parts := strings.Split(raw, "/")
	for i, part := range parts {
		module, name, value := splitSegment(part)
		if name == "" {
			return nil, malformedRequest("empty node name in %q", part)
		}

		children, err := s.router.Children(parent)
		if err != nil {
			return nil, unknownElement(name)
		}
		node := schemaChild(children, name)
		if node == nil {
			return nil, unknownElement(name)
		}

		segment := router.Segment{Name: name}
		canonical := name
		collection := false
		switch {
		case node.Kind == router.KindList && value == "":
			if i != len(parts)-1 {
				return nil, malformedRequest("list %s is missing its key predicate", name)
			}
			collection = true
		case node.Kind == router.KindList:
			segment.Key, segment.Value = node.Key, value
			canonical = name + "[" + node.Key + "=" + value + "]"
		case value != "":
			return nil, malformedRequest("%s is not a list", name)
		}

		path := joinPath(parent, canonical)
		if module != "" && module != router.ModuleFor(path).Name {
			return nil, malformedRequest("module %q does not own %s", module, name)
		}

		target.BasePath = parent
		target.Name = name
		target.Segment = segment
		target.Node = *node
		target.Path = path
		target.Collection = collection

		if collection {
			return target, nil
		}
		parent = path
	}
	return target, nil
}

// splitSegment splits one URL path segment into its optional module prefix and
// its optional RESTCONF list key, as in sim-l2-switching:vlan=100.
func splitSegment(part string) (module, name, value string) {
	name = part
	if index := strings.IndexByte(part, '='); index >= 0 {
		name, value = part[:index], part[index+1:]
	}
	if index := strings.IndexByte(name, ':'); index >= 0 {
		module, name = name[:index], name[index+1:]
	}
	return module, name, value
}

// parseDatastore maps the ?datastore= parameter to a datastore.
func parseDatastore(value string) (store.Datastore, *httpError) {
	switch value {
	case "", string(store.Running):
		return store.Running, nil
	case string(store.Candidate):
		return store.Candidate, nil
	case string(store.Startup):
		return store.Startup, nil
	default:
		return "", malformedRequest("unknown datastore %q", value)
	}
}

// parseContent maps the ?content= parameter to a content selection.
func parseContent(value string) (content, *httpError) {
	switch value {
	case "", string(contentAll):
		return contentAll, nil
	case string(contentConfig):
		return contentConfig, nil
	case string(contentState):
		return contentState, nil
	default:
		return "", malformedRequest("unknown content %q", value)
	}
}

// schemaChild returns the schema child named name, or nil.
func schemaChild(children []router.Node, name string) *router.Node {
	for i := range children {
		if children[i].Name == name {
			return &children[i]
		}
	}
	return nil
}

// joinPath appends one canonical segment to a router path.
func joinPath(prefix, segment string) string {
	if prefix == "" {
		return segment
	}
	return prefix + "/" + segment
}
