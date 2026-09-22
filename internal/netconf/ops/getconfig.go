package ops

import (
	"context"

	"github.com/DisMosGit/triadsim/internal/datatree"
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

	root, err := datatree.Read(ctx, deps.Router, datastore, datatree.ReadOptions{})
	if err != nil {
		return nil, fromDataTree(err)
	}

	nodes := root.Children
	if filter != nil {
		nodes = pruneNodes(nodes, filter.level())
	}

	data := NewElement("data")
	renderNodes(data, nodes, "")
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

// renderNodes appends the XML elements of nodes to parent and reports whether
// it appended anything. currentNS is the namespace the parent already declares;
// an element declares the module namespace only when it differs.
func renderNodes(parent *Element, nodes []*datatree.Node, currentNS string) bool {
	appended := false
	for _, node := range nodes {
		switch node.Kind {
		case router.KindLeaf:
			element, _ := elementFor(node.Path, node.Name, currentNS)
			element.Text = datatree.FormatLeaf(node.Value)
			parent.Append(element)
			appended = true

		case router.KindContainer:
			element, childNS := elementFor(node.Path, node.Name, currentNS)
			if !renderNodes(element, node.Children, childNS) {
				continue
			}
			parent.Append(element)
			appended = true

		case router.KindList:
			for _, entry := range node.Children {
				element, entryNS := elementFor(entry.Path, node.Name, currentNS)
				if !renderNodes(element, entry.Children, entryNS) {
					continue
				}
				parent.Append(element)
				appended = true
			}
		}
	}
	return appended
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

// pruneNodes keeps the parts of a read tree the filter selects. It applies the
// same rules the walker used: a level that selects all keeps every child, a
// leaf matches on content when the filter carries text, and an empty container
// or list disappears.
func pruneNodes(nodes []*datatree.Node, level filterLevel) []*datatree.Node {
	kept := make([]*datatree.Node, 0, len(nodes))
	for _, node := range nodes {
		matched, childLevel, ok := level.child(node.Name)
		if !ok {
			continue
		}

		switch node.Kind {
		case router.KindLeaf:
			if matched != nil && matched.hasText && !datatree.ContentMatches(node.Leaf, matched.text, node.Value) {
				continue
			}
			kept = append(kept, node)

		case router.KindContainer:
			inner := pruneNodes(node.Children, childLevel)
			if len(inner) == 0 {
				continue
			}
			node.Children = inner
			kept = append(kept, node)

		case router.KindList:
			entries := pruneEntries(node, matched, childLevel)
			if len(entries) == 0 {
				continue
			}
			node.Children = entries
			kept = append(kept, node)
		}
	}
	return kept
}

// pruneEntries keeps the list entries the filter selects. The key leaf is
// always kept, as YANG XML encoding requires; the remaining children are
// narrowed by the list filter's children.
func pruneEntries(list *datatree.Node, node *filterNode, level filterLevel) []*datatree.Node {
	kept := make([]*datatree.Node, 0, len(list.Children))
	for _, entry := range list.Children {
		if !entrySelected(node, entry, list.Key) {
			continue
		}

		children := make([]*datatree.Node, 0, len(entry.Children))
		rest := make([]*datatree.Node, 0, len(entry.Children))
		for _, child := range entry.Children {
			if child.Name == list.Key {
				children = append(children, child)
				continue
			}
			rest = append(rest, child)
		}
		children = append(children, pruneNodes(rest, level)...)
		if len(children) == 0 {
			continue
		}
		entry.Children = children
		kept = append(kept, entry)
	}
	return kept
}

// entrySelected reports whether a filter selects a list entry. Without a filter
// node, or without a key leaf in it, every entry is selected.
func entrySelected(node *filterNode, entry *datatree.Node, keyName string) bool {
	if node == nil {
		return true
	}
	for _, child := range node.children {
		if child.name != keyName || !child.hasText {
			continue
		}
		key := entry.Child(keyName)
		if key == nil {
			return false
		}
		return datatree.ContentMatches(key.Leaf, child.text, key.Value)
	}
	return true
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
