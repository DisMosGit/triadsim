package gnmi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DisMosGit/triadsim/internal/router"
)

// origins are the gNMI origin labels the server accepts: an empty origin, one
// of the simulator's module names (sim-device, sim-radio-link, ...) or their
// XML namespaces. An unknown origin is rejected rather than ignored, so a
// client targeting another device's schema learns about it.
func originAccepted(origin string) bool {
	if origin == "" {
		return true
	}
	for _, module := range modules {
		if origin == module.Name || origin == module.Namespace {
			return true
		}
	}
	return false
}

// modules are the model modules gNMI exposes, used for both the origin check
// and the capability models.
var modules = []router.Module{
	router.ModuleDevice,
	router.ModuleRadioLink,
	router.ModuleL2,
	router.ModuleSync,
}

// routerPath converts a gNMI path into the canonical router path of the same
// node. A nil, empty or root path addresses the model root and returns "".
//
// Path elements carry the model node names; a module prefix ("sim-device:
// system-info") is stripped exactly as the RESTCONF JSON codec strips it, and a
// single key renders as the [key=value] predicate the router grammar uses. The
// deprecated repeated-string element list is accepted as the literal path it
// spells.
func routerPath(path *gnmi.Path) (string, error) {
	if path == nil {
		return "", nil
	}
	if !originAccepted(path.GetOrigin()) {
		return "", status.Errorf(codes.InvalidArgument, "unknown origin %q", path.GetOrigin())
	}

	if elems := path.GetElem(); len(elems) > 0 {
		return elementsToPath(elems)
	}
	if elements := path.GetElement(); len(elements) > 0 {
		joined := strings.Join(elements, "/")
		if _, err := router.Parse(joined); err != nil {
			return "", status.Errorf(codes.InvalidArgument, "invalid path %q: %v", joined, err)
		}
		return joined, nil
	}
	return "", nil
}

// elementsToPath renders gNMI path elements as a router path.
func elementsToPath(elems []*gnmi.PathElem) (string, error) {
	segments := make([]string, 0, len(elems))
	for _, elem := range elems {
		name := stripModule(elem.GetName())
		if name == "" {
			return "", status.Error(codes.InvalidArgument, "path element has no name")
		}
		keys := elem.GetKey()
		switch len(keys) {
		case 0:
			segments = append(segments, name)
		case 1:
			key, value := onlyKey(keys)
			if value == "" {
				return "", status.Errorf(codes.InvalidArgument, "path element %s has an empty %s key", name, key)
			}
			segments = append(segments, fmt.Sprintf("%s[%s=%s]", name, key, value))
		default:
			return "", status.Errorf(codes.InvalidArgument,
				"path element %s has %d keys; the model addresses list entries by one key", name, len(keys))
		}
	}
	joined := strings.Join(segments, "/")
	if _, err := router.Parse(joined); err != nil {
		return "", status.Errorf(codes.InvalidArgument, "invalid path %q: %v", joined, err)
	}
	return joined, nil
}

// onlyKey returns the single entry of a key map, whose name is the sorted first
// key so the result is deterministic.
func onlyKey(keys map[string]string) (string, string) {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	return names[0], keys[names[0]]
}

// stripModule removes the "module:" prefix a gNMI element name may carry.
func stripModule(name string) string {
	if index := strings.IndexByte(name, ':'); index >= 0 {
		return name[index+1:]
	}
	return name
}

// gnmiPath is the inverse of routerPath: the canonical router path of a model
// node becomes gNMI path elements, one per segment, with the list key as the
// element's key map. The root path becomes an empty message.
func gnmiPath(path string) (*gnmi.Path, error) {
	if path == "" {
		return &gnmi.Path{}, nil
	}

	parsed, err := router.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("gnmi: convert path %q: %w", path, err)
	}

	elems := make([]*gnmi.PathElem, 0, len(parsed.Segments))
	for _, segment := range parsed.Segments {
		elem := &gnmi.PathElem{Name: segment.Name}
		if segment.Key != "" {
			elem.Key = map[string]string{segment.Key: segment.Value}
		}
		elems = append(elems, elem)
	}
	return &gnmi.Path{Elem: elems}, nil
}

// joinPrefix returns the router path of rel below prefix. Either part may be
// empty, which is the model root.
func joinPrefix(prefix string, rel *gnmi.Path) (string, error) {
	path, err := routerPath(rel)
	if err != nil {
		return "", err
	}
	switch {
	case prefix == "":
		return path, nil
	case path == "":
		return prefix, nil
	default:
		return prefix + "/" + path, nil
	}
}

// parseSegments parses a router path for prefix comparison, reporting whether
// it is usable.
func parseSegments(path string) ([]router.Segment, bool) {
	if path == "" {
		return nil, true
	}
	parsed, err := router.Parse(path)
	if err != nil {
		return nil, false
	}
	return parsed.Segments, true
}

// isPrefix reports whether outer is outer-or-equal to inner, segment by
// segment, so [name=eth0] never matches [name=eth01].
func isPrefix(outer, inner []router.Segment) bool {
	if len(outer) > len(inner) {
		return false
	}
	for i := range outer {
		if outer[i] != inner[i] {
			return false
		}
	}
	return true
}

// pathsOverlap reports whether one of the two canonical paths lies on the other
// one, which is the condition for a subscription to receive an update about an
// affected node.
func pathsOverlap(a, b string) bool {
	aSegments, ok := parseSegments(a)
	if !ok {
		return false
	}
	bSegments, ok := parseSegments(b)
	if !ok {
		return false
	}
	return isPrefix(aSegments, bSegments) || isPrefix(bSegments, aSegments)
}

// checkPath verifies that a canonical path exists in the model schema, so a Get
// or a subscription of an unknown node answers NotFound instead of looking like
// an empty subtree.
func checkPath(r *router.Router, path string) error {
	if path == "" {
		return nil
	}
	parsed, err := router.Parse(path)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid path %q: %v", path, err)
	}
	last := parsed.Segments[len(parsed.Segments)-1]
	_, err = schemaKind(r, parsed.Segments[:len(parsed.Segments)-1], last.Name)
	return err
}

// schemaKind resolves the schema kind of one segment below its parent path. An
// unknown parent path and an unknown segment both report NotFound.
func schemaKind(r *router.Router, parent []router.Segment, name string) (router.Kind, error) {
	children, err := r.Children(router.Path{Segments: parent}.String())
	if err != nil {
		return "", statusError(err)
	}
	for _, child := range children {
		if child.Name == name {
			return child.Kind, nil
		}
	}
	return "", status.Errorf(codes.NotFound, "unknown element %s", name)
}
