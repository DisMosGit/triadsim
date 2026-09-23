package restconf

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Content selections accepted by the ?content= query parameter (RFC 8040
// §4.8.2). They are the datatree content selections.
const (
	contentAll    = datatree.ContentAll
	contentConfig = datatree.ContentConfig
	contentState  = datatree.ContentNonConfig
)

// Datastore identityrefs accepted in a /restconf/ds/ segment. RFC 8527 §3.1
// encodes the datastore as a namespace-qualified identity derived from
// ietf-datastores:datastore; startup is not an NMDA datastore, so the
// simulator derives its own identity in the sim-device module.
const (
	identityRunning   = "ietf-datastores:running"
	identityCandidate = "ietf-datastores:candidate"
	identityStartup   = "sim-device:startup"
)

// target is one parsed RESTCONF request target: the resource path resolved
// against the router schema plus the datastore and content selection.
type target struct {
	// Base is the addressing root of the request, what a Location header is
	// built under: {+restconf}/data or {+restconf}/ds/<identityref>.
	Base string
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
	Content datatree.Content
}

// parseTarget resolves r's URL against the router schema. A malformed path or
// an unknown node is reported as one of the RESTCONF error responses.
//
// The request path is read in its escaped form and split before percent-
// decoding (RFC 8040 §3.5.3), so an encoded "/" or "=" inside a list key
// stays part of the key instead of corrupting the segment structure.
func (s *Server) parseTarget(r *http.Request) (*target, *httpError) {
	base, raw, datastore, httpErr := resolveAddressing(r)
	if httpErr != nil {
		return nil, httpErr
	}
	selected, httpErr := parseContent(r.URL.Query().Get("content"))
	if httpErr != nil {
		return nil, httpErr
	}

	target := &target{Base: base, Datastore: datastore, Content: selected}

	raw = strings.Trim(raw, "/")
	if raw == "" {
		return target, nil // the datastore root
	}

	parent := ""
	parts := strings.Split(raw, "/")
	for i, part := range parts {
		module, name, value, httpErr := splitSegment(part)
		if httpErr != nil {
			return nil, httpErr
		}
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

// resolveAddressing returns the addressing root of the request (what a
// Location header is built under), the still-escaped data path and the
// addressed datastore.
//
// Two addressings are accepted. {+restconf}/data is the RFC 8040 datastore
// resource; the datastore is selected with the ?datastore= alias (default
// running). {+restconf}/ds/<identityref> carries the datastore in the path as
// an RFC 8527 §3.1 identityref. Mixing the two is ambiguous and rejected.
func resolveAddressing(r *http.Request) (base, raw string, ds store.Datastore, httpErr *httpError) {
	path := r.URL.EscapedPath()
	query := r.URL.Query().Get("datastore")

	switch {
	case strings.HasPrefix(path, BasePath+"/data"):
		rest := strings.TrimPrefix(path, BasePath+"/data")
		if rest != "" && !strings.HasPrefix(rest, "/") {
			return "", "", "", notFound(path)
		}
		datastore, httpErr := parseDatastore(query)
		if httpErr != nil {
			return "", "", "", httpErr
		}
		return BasePath + "/data", rest, datastore, nil

	case strings.HasPrefix(path, BasePath+"/ds/"):
		if query != "" {
			return "", "", "", malformedRequest(
				"?datastore= and /restconf/ds/ address the same thing; use one")
		}
		rest := strings.TrimPrefix(path, BasePath+"/ds/")
		identity := rest
		if index := strings.IndexByte(rest, '/'); index >= 0 {
			identity, raw = rest[:index], rest[index:]
		}
		datastore, httpErr := parseDatastoreIdentity(identity)
		if httpErr != nil {
			return "", "", "", httpErr
		}
		return BasePath + "/ds/" + identity, raw, datastore, nil

	default:
		return "", "", "", notFound(path)
	}
}

// splitSegment splits one (still percent-encoded) URL path segment into its
// optional module prefix, its node name and its optional RESTCONF list key,
// as in sim-l2-switching:vlan=100. The split happens before percent-decoding,
// so a key value may itself contain encoded "=" or "/" (RFC 8040 §3.5.3).
func splitSegment(part string) (module, name, value string, httpErr *httpError) {
	rawName := part
	rawValue := ""
	if index := strings.IndexByte(part, '='); index >= 0 {
		rawName, rawValue = part[:index], part[index+1:]
	}
	rawModule := ""
	if index := strings.IndexByte(rawName, ':'); index >= 0 {
		rawModule, rawName = rawName[:index], rawName[index+1:]
	}

	module, err := url.PathUnescape(rawModule)
	if err != nil {
		return "", "", "", malformedRequest("malformed percent-encoding in %q", part)
	}
	name, err = url.PathUnescape(rawName)
	if err != nil {
		return "", "", "", malformedRequest("malformed percent-encoding in %q", part)
	}
	value, err = url.PathUnescape(rawValue)
	if err != nil {
		return "", "", "", malformedRequest("malformed percent-encoding in %q", part)
	}
	return module, name, value, nil
}

// parseDatastore maps the ?datastore= alias to a datastore.
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

// parseDatastoreIdentity maps the namespace-qualified identityref of a
// /restconf/ds/ segment to a datastore (RFC 8527 §3.1). Unlike the
// ?datastore= alias, only the qualified form is accepted.
func parseDatastoreIdentity(value string) (store.Datastore, *httpError) {
	switch value {
	case identityRunning:
		return store.Running, nil
	case identityCandidate:
		return store.Candidate, nil
	case identityStartup:
		return store.Startup, nil
	default:
		return "", malformedRequest("unknown datastore %q", value)
	}
}

// parseContent maps the ?content= parameter to a content selection.
func parseContent(value string) (datatree.Content, *httpError) {
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

// locationFor converts a canonical router path into the RESTCONF URL of a
// resource under base: the top node is module-qualified and list keys are
// rendered as =value instead of the [key=value] predicate, with the key value
// percent-encoded (RFC 8040 §3.5.3).
func locationFor(base, path string) string {
	parsed, err := router.Parse(path)
	if err != nil {
		return base
	}

	var builder strings.Builder
	builder.WriteString(base + "/")
	for i, segment := range parsed.Segments {
		if i > 0 {
			builder.WriteByte('/')
		}
		if i == 0 {
			builder.WriteString(router.ModuleFor(path).Name)
			builder.WriteByte(':')
		}
		builder.WriteString(segment.Name)
		if segment.Key != "" {
			builder.WriteByte('=')
			builder.WriteString(escapeKey(segment.Value))
		}
	}
	return builder.String()
}

// escapeKey percent-encodes one list key value for a RESTCONF resource
// identifier (RFC 8040 §3.5.3): every reserved character — the comma above
// all — is percent-encoded. Only the RFC 3986 unreserved set stays literal,
// which is stricter than url.PathEscape (it leaves "= : ; + @" alone).
func escapeKey(value string) string {
	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			builder.WriteByte(c)
		default:
			fmt.Fprintf(&builder, "%%%02X", c)
		}
	}
	return builder.String()
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
