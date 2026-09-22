package datatree

import "github.com/DisMosGit/triadsim/internal/router"

// Document is a parsed request body: a protocol-neutral tree of nodes with a
// name, optional character data, optional attributes (the NETCONF operation
// attribute) and children. Whitespace handling and XML namespaces are the
// caller's problem; the engine matches nodes by name against the router
// schema.
type Document struct {
	Name     string
	Text     string
	HasText  bool
	Attrs    map[string]string
	Children []*Document
}

// Child returns the first child named name, or nil.
func (d *Document) Child(name string) *Document {
	for _, child := range d.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

// ChildrenNamed returns every child named name.
func (d *Document) ChildrenNamed(name string) []*Document {
	var children []*Document
	for _, child := range d.Children {
		if child.Name == name {
			children = append(children, child)
		}
	}
	return children
}

// Attr returns an attribute by its local name, for example "operation".
func (d *Document) Attr(name string) (string, bool) {
	value, ok := d.Attrs[name]
	return value, ok
}

// Node is one resolved node of a data tree. Path is the canonical router path
// (list entries carry their key predicate); a list is one Node whose Children
// are its entries, and every entry carries its KeyValue.
type Node struct {
	Path     string
	Name     string
	Kind     router.Kind
	Key      string
	KeyValue string
	Leaf     router.LeafKind
	Value    any
	Children []*Node
}

// Child returns the first child named name, or nil.
func (n *Node) Child(name string) *Node {
	for _, child := range n.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}
