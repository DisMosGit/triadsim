package ops

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// GetConfig implements the <get-config> operation: it returns the configuration
// data of one datastore as a <data> element, optionally narrowed by a subtree
// filter.
//
// Only configuration data is returned. Leaves tagged config:"false" hold state
// (counters, measured levels) and are omitted, because the simulator has no
// separate state datastore and <get> is out of scope.
func GetConfig(ctx context.Context, deps Deps, operation *Element) (*Element, error) {
	datastore, opErr := sourceDatastore(operation)
	if opErr != nil {
		return nil, opErr
	}
	filter, opErr := parseFilter(operation.Child("filter"))
	if opErr != nil {
		return nil, opErr
	}

	values, err := collectValues(ctx, deps, datastore)
	if err != nil {
		return nil, err
	}

	data := NewElement("data")
	w := &walker{deps: deps, values: values}
	if _, err := w.build(ctx, data, "", filter.level(), "", ""); err != nil {
		return nil, err
	}
	return data, nil
}

// sourceDatastore maps the <source> element of get-config to a datastore.
func sourceDatastore(operation *Element) (store.Datastore, *Error) {
	source := operation.Child("source")
	if source == nil {
		return "", MissingElement("source")
	}
	return datastoreElement(source)
}

// datastoreElement maps the single child of a <source> or <target> element, for
// example <candidate/>, to a datastore.
func datastoreElement(container *Element) (store.Datastore, *Error) {
	switch len(container.Children) {
	case 0:
		return "", MissingElement("datastore")
	case 1:
	default:
		return "", InvalidValue("exactly one datastore is expected")
	}

	switch name := container.Children[0].Name; name {
	case "running":
		return store.Running, nil
	case "candidate":
		return store.Candidate, nil
	case "startup":
		return store.Startup, nil
	default:
		return "", UnknownElement(name)
	}
}

// collectValues returns every leaf of one datastore keyed by its canonical
// router path.
func collectValues(ctx context.Context, deps Deps, ds store.Datastore) (map[string]router.Result, error) {
	results, err := deps.Router.List(ctx, ds, "")
	if err != nil {
		return nil, Failed(err)
	}

	values := make(map[string]router.Result, len(results))
	for _, result := range results {
		values[result.Path] = result
	}
	return values, nil
}

// filter is a parsed <filter>. A nil filter selects the whole datastore.
type filter struct {
	nodes []*filterNode
}

// filterNode is one element of a subtree filter. text, when set, is a
// content-match value for a leaf or for a list key.
type filterNode struct {
	name     string
	text     string
	hasText  bool
	children []*filterNode
}

// parseFilter parses the optional <filter> of get-config. Only the default
// subtree filter is implemented.
func parseFilter(element *Element) (*filter, *Error) {
	if element == nil {
		return nil, nil
	}
	if kind, ok := element.Attr("type"); ok && kind != "subtree" {
		return nil, NotSupported("filter type %q is not supported (only subtree)", kind)
	}

	parsed := &filter{}
	for _, child := range element.Children {
		parsed.nodes = append(parsed.nodes, filterNodeOf(child))
	}
	return parsed, nil
}

// filterNodeOf converts one XML element into a filter node.
func filterNodeOf(element *Element) *filterNode {
	node := &filterNode{name: element.Name}
	if text := element.TrimmedText(); text != "" {
		node.text, node.hasText = text, true
	}
	for _, child := range element.Children {
		node.children = append(node.children, filterNodeOf(child))
	}
	return node
}

// level returns the filter state at the top of the data tree.
func (f *filter) level() filterLevel {
	if f == nil {
		return filterLevel{}
	}
	return filterLevel{active: true, nodes: f.nodes}
}

// filterLevel is the filter state at one level of the walk. An inactive level
// selects every child; an active level with no nodes selects the whole subtree.
type filterLevel struct {
	active bool
	nodes  []*filterNode
}

// selectsAll reports whether the level selects every descendant.
func (l filterLevel) selectsAll() bool {
	return !l.active || len(l.nodes) == 0
}

// child returns the filter node named name and the level of its children. ok is
// false when an active filter does not name the child, which drops it.
func (l filterLevel) child(name string) (*filterNode, filterLevel, bool) {
	if l.selectsAll() {
		return nil, filterLevel{}, true
	}
	for _, node := range l.nodes {
		if node.name == name {
			return node, filterLevel{active: true, nodes: node.children}, true
		}
	}
	return nil, filterLevel{}, false
}

// walker walks the schema, the stored values and the filter together and builds
// the data element of the reply.
type walker struct {
	deps   Deps
	values map[string]router.Result
}

// build appends the selected children of the node at prefix to parent and
// reports whether it appended anything. skip names a child that must not be
// emitted, which is how a list entry skips the key it already wrote.
func (w *walker) build(ctx context.Context, parent *Element, prefix string, level filterLevel, ns, skip string) (bool, error) {
	children, err := w.deps.Router.Children(prefix)
	if err != nil {
		return false, Failed(err)
	}

	appended := false
	for _, child := range children {
		if child.Name == skip {
			continue
		}
		node, childLevel, ok := level.child(child.Name)
		if !ok {
			continue
		}
		path := join(prefix, child.Name)

		switch child.Kind {
		case router.KindLeaf:
			result, ok := w.values[path]
			if !ok || !result.Writable {
				continue
			}
			if node != nil && node.hasText && !contentMatches(child.Leaf, node.text, result.Value) {
				continue
			}
			element, _ := elementFor(path, child.Name, ns)
			element.Text = formatLeaf(result.Value)
			parent.Append(element)
			appended = true

		case router.KindContainer:
			element, childNS := elementFor(path, child.Name, ns)
			inner, err := w.build(ctx, element, path, childLevel, childNS, "")
			if err != nil {
				return false, err
			}
			if !inner {
				continue
			}
			parent.Append(element)
			appended = true

		case router.KindList:
			wrote, err := w.buildList(ctx, parent, prefix, child, node, childLevel, ns)
			if err != nil {
				return false, err
			}
			appended = appended || wrote
		}
	}
	return appended, nil
}

// buildList appends one element per selected list instance.
func (w *walker) buildList(ctx context.Context, parent *Element, prefix string, list router.Node, node *filterNode, level filterLevel, ns string) (bool, error) {
	entries := instancesOf(w.values, prefix, list.Name)
	if len(entries) == 0 {
		return false, nil
	}

	// The schema of a list entry is the same for every instance, so the first
	// one is enough to find the key leaf and sort by it.
	entryChildren, err := w.deps.Router.Children(entries[0].path)
	if err != nil {
		return false, Failed(err)
	}
	keyNode := nodeNamed(entryChildren, list.Key)
	if keyNode == nil {
		return false, Failed(fmt.Errorf("list %s has no key node %s", list.Name, list.Key))
	}
	sortEntries(entries, keyNode.Leaf)

	appended := false
	for _, entry := range entries {
		if !entrySelected(node, entry, *keyNode, w.values) {
			continue
		}
		element, entryNS := elementFor(entry.path, list.Name, ns)
		wrote, err := w.buildEntry(ctx, element, entry, level, entryNS, list.Key)
		if err != nil {
			return false, err
		}
		if !wrote {
			continue
		}
		parent.Append(element)
		appended = true
	}
	return appended, nil
}

// buildEntry writes one list entry: the key leaf first, as YANG XML encoding
// requires, then every selected child.
func (w *walker) buildEntry(ctx context.Context, element *Element, entry listEntry, level filterLevel, ns, keyName string) (bool, error) {
	appended := false

	keyPath := join(entry.path, keyName)
	if result, ok := w.values[keyPath]; ok && result.Writable {
		keyElement, _ := elementFor(keyPath, keyName, ns)
		keyElement.Text = formatLeaf(result.Value)
		element.Append(keyElement)
		appended = true
	}

	inner, err := w.build(ctx, element, entry.path, level, ns, keyName)
	if err != nil {
		return false, err
	}
	return appended || inner, nil
}

// listEntry is one instance of a list in the datastore.
type listEntry struct {
	segment string // interface[name=radio0]
	key     string // radio0
	path    string // interfaces/interface[name=radio0]
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

		entry := listEntry{segment: text, key: segment.Value}
		if prefix == "" {
			entry.path = text
		} else {
			entry.path = prefix + "/" + text
		}
		entries = append(entries, entry)
	}
	return entries
}

// entrySelected reports whether a filter selects a list entry. Without a filter
// node, or without a key leaf in it, every entry is selected.
func entrySelected(node *filterNode, entry listEntry, keyNode router.Node, values map[string]router.Result) bool {
	if node == nil {
		return true
	}
	for _, child := range node.children {
		if child.name != keyNode.Name || !child.hasText {
			continue
		}
		result, ok := values[join(entry.path, keyNode.Name)]
		if !ok {
			return false
		}
		return contentMatches(keyNode.Leaf, child.text, result.Value)
	}
	return true
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

// elementFor builds an element and declares the module namespace when it
// differs from the one the parent already declared. The returned string is the
// effective namespace for the element's children.
func elementFor(path, name, currentNS string) (*Element, string) {
	element := &Element{Name: name}
	ns := namespaceFor(path)
	if ns == currentNS {
		return element, currentNS
	}
	element.Space = ns
	return element, ns
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
