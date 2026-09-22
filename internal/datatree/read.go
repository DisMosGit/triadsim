package datatree

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// ReadOptions selects the subtree and the access a Read returns.
type ReadOptions struct {
	// Prefix is a router path. An empty Prefix addresses the model root and
	// returns a synthetic container whose Children are the root's children.
	Prefix string
	// State includes leaves tagged config:"false" (counters, measured levels).
	// NETCONF get-config leaves them out; RESTCONF GET returns them by default.
	State bool
}

// Read returns the node at opts.Prefix in ds, resolved against the router
// schema. A Prefix that addresses a leaf or a list entry the datastore does not
// hold returns (nil, nil): "no data" is not an error, the caller decides
// whether that is a 404. A list is one KindList node whose Children are its
// entries, keyed by the list's key predicate.
func Read(ctx context.Context, r *router.Router, ds store.Datastore, opts ReadOptions) (*Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	values, err := collect(ctx, r, ds)
	if err != nil {
		return nil, err
	}

	if opts.Prefix == "" {
		children, err := buildChildren(r, values, "", opts.State)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: router.KindContainer, Children: children}, nil
	}

	parent, last, err := splitPrefix(opts.Prefix)
	if err != nil {
		return nil, err
	}
	children, err := buildChildren(r, values, parent, opts.State)
	if err != nil {
		return nil, err
	}
	for _, node := range children {
		if node.Name != last.Name {
			continue
		}
		if node.Kind == router.KindList {
			// A segment without a key addresses the whole list (its collection
			// resource); with a key it selects one entry.
			if last.Key == "" {
				return node, nil
			}
			return listEntryNode(node, last), nil
		}
		return node, nil
	}
	return nil, nil
}

// listEntryNode narrows a list node to the one entry selected by seg, or nil
// when the datastore does not hold it.
func listEntryNode(list *Node, seg router.Segment) *Node {
	for _, entry := range list.Children {
		if entry.KeyValue == seg.Value {
			return &Node{
				Path:     list.Path,
				Name:     list.Name,
				Kind:     router.KindList,
				Key:      list.Key,
				Children: []*Node{entry},
			}
		}
	}
	return nil
}

// buildChildren returns one node per schema child of prefix that has data.
// Containers are always returned (possibly empty) and list nodes carry their
// entries, so a renderer can decide what an empty container means.
func buildChildren(r *router.Router, values map[string]router.Result, prefix string, state bool) ([]*Node, error) {
	children, err := r.Children(prefix)
	if err != nil {
		return nil, err
	}

	nodes := make([]*Node, 0, len(children))
	for _, child := range children {
		path := join(prefix, child.Name)
		switch child.Kind {
		case router.KindLeaf:
			result, ok := values[path]
			if !ok || (!state && !result.Writable) {
				continue
			}
			nodes = append(nodes, &Node{
				Path:  path,
				Name:  child.Name,
				Kind:  router.KindLeaf,
				Leaf:  child.Leaf,
				Value: result.Value,
			})

		case router.KindContainer:
			inner, err := buildChildren(r, values, path, state)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, &Node{Path: path, Name: child.Name, Kind: router.KindContainer, Children: inner})

		case router.KindList:
			entries, err := buildEntries(r, values, prefix, child, state)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, &Node{Path: path, Name: child.Name, Kind: router.KindList, Key: child.Key, Children: entries})
		}
	}
	return nodes, nil
}

// buildEntries returns the list instances present in values, ordered by key,
// each with its key leaf first. The order is what makes list output stable.
func buildEntries(r *router.Router, values map[string]router.Result, prefix string, list router.Node, state bool) ([]*Node, error) {
	instances := instancesOf(values, prefix, list.Name)
	if len(instances) == 0 {
		return nil, nil
	}

	// The schema of a list entry is the same for every instance, so the first
	// one is enough to find the key leaf and sort by it.
	entryChildren, err := r.Children(instances[0].path)
	if err != nil {
		return nil, err
	}
	keyNode := nodeNamed(entryChildren, list.Key)
	if keyNode == nil {
		return nil, fmt.Errorf("list %s has no key node %s", list.Name, list.Key)
	}
	sortEntries(instances, keyNode.Leaf)

	entries := make([]*Node, 0, len(instances))
	for _, instance := range instances {
		entry := &Node{
			Path:     instance.path,
			Name:     list.Name,
			Kind:     router.KindContainer,
			Key:      list.Key,
			KeyValue: instance.key,
		}

		keyPath := join(instance.path, list.Key)
		if result, ok := values[keyPath]; ok && (state || result.Writable) {
			entry.Children = append(entry.Children, &Node{
				Path:  keyPath,
				Name:  list.Key,
				Kind:  router.KindLeaf,
				Leaf:  keyNode.Leaf,
				Value: result.Value,
			})
		}

		inner, err := buildChildren(r, values, instance.path, state)
		if err != nil {
			return nil, err
		}
		for _, child := range inner {
			if child.Name == list.Key {
				continue
			}
			entry.Children = append(entry.Children, child)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// collect returns every leaf of one datastore keyed by its canonical router
// path.
func collect(ctx context.Context, r *router.Router, ds store.Datastore) (map[string]router.Result, error) {
	results, err := r.List(ctx, ds, "")
	if err != nil {
		return nil, Failed(err)
	}

	values := make(map[string]router.Result, len(results))
	for _, result := range results {
		values[result.Path] = result
	}
	return values, nil
}

// listEntry is one instance of a list in the datastore.
type listEntry struct {
	path string // vlans/vlan[id=100]
	key  string // 100
}

// instancesOf returns the list instances that appear in values below prefix.
// The order is unspecified; sortEntries orders them.
func instancesOf(values map[string]router.Result, prefix, name string) []listEntry {
	depth := 0
	if prefix != "" {
		parsed, err := router.Parse(prefix)
		if err != nil {
			return nil
		}
		depth = len(parsed.Segments)
	}

	needle := prefix + "/"
	if prefix == "" {
		needle = ""
	}

	seen := make(map[string]struct{})
	var entries []listEntry
	for path := range values {
		if !strings.HasPrefix(path, needle) {
			continue
		}
		parsed, err := router.Parse(path)
		if err != nil || len(parsed.Segments) <= depth {
			continue
		}
		segment := parsed.Segments[depth]
		if segment.Name != name || segment.Key == "" {
			continue
		}
		text := segment.Name + "[" + segment.Key + "=" + segment.Value + "]"
		if _, duplicate := seen[text]; duplicate {
			continue
		}
		seen[text] = struct{}{}

		entry := listEntry{key: segment.Value}
		if prefix == "" {
			entry.path = text
		} else {
			entry.path = prefix + "/" + text
		}
		entries = append(entries, entry)
	}
	return entries
}

// sortEntries orders list entries by key, numerically when the key is numeric.
func sortEntries(entries []listEntry, kind router.LeafKind) {
	numeric := kind != router.LeafString && kind != router.LeafBool
	sort.Slice(entries, func(i, j int) bool {
		if numeric {
			left, lerr := strconv.ParseFloat(entries[i].key, 64)
			right, rerr := strconv.ParseFloat(entries[j].key, 64)
			if lerr == nil && rerr == nil {
				return left < right
			}
		}
		return entries[i].key < entries[j].key
	})
}

// splitPrefix splits a router path into the parent path and its last segment.
func splitPrefix(path string) (string, router.Segment, error) {
	parsed, err := router.Parse(path)
	if err != nil {
		return "", router.Segment{}, err
	}
	segments := parsed.Segments
	last := segments[len(segments)-1]
	if len(segments) == 1 {
		return "", last, nil
	}
	return router.Path{Segments: segments[:len(segments)-1]}.String(), last, nil
}

// nodeNamed returns the schema child named name, or nil.
func nodeNamed(children []router.Node, name string) *router.Node {
	for i := range children {
		if children[i].Name == name {
			return &children[i]
		}
	}
	return nil
}

// join appends one segment to a router path.
func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}
