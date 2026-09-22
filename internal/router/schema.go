package router

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Kind classifies a schema node for protocol codecs. It mirrors the YANG data
// node kinds the model's path tags describe.
type Kind string

// Schema node kinds.
const (
	// KindContainer is a struct node that holds children.
	KindContainer Kind = "container"
	// KindList is a slice node whose entries are selected by a key predicate.
	KindList Kind = "list"
	// KindLeaf is a scalar node that holds one value.
	KindLeaf Kind = "leaf"
)

// LeafKind names the Go type of a leaf. The names match the store's leaf kinds,
// so a protocol codec can parse a wire value into exactly the type the store
// holds.
type LeafKind string

// Leaf kinds, one per supported Go leaf type.
const (
	LeafBool    LeafKind = "bool"
	LeafInt     LeafKind = "int"
	LeafUint8   LeafKind = "uint8"
	LeafUint16  LeafKind = "uint16"
	LeafUint32  LeafKind = "uint32"
	LeafUint64  LeafKind = "uint64"
	LeafFloat64 LeafKind = "float64"
	LeafString  LeafKind = "string"
)

// Node describes one child of a model node. Protocol codecs use it to map their
// own tree — for example the XML elements of NETCONF and RESTCONF — to the
// router paths of the same node.
type Node struct {
	// Name is the path segment name, which is also the element name a
	// protocol codec uses for the node.
	Name string
	// Kind classifies the node.
	Kind Kind
	// Key is the key predicate name of a list, for example "name" for
	// interfaces/interface[name=radio0]. It is empty for other kinds.
	Key string
	// Leaf is the leaf's type. It is only set for KindLeaf.
	Leaf LeafKind
}

// schemaNode is one node of the model schema tree.
type schemaNode struct {
	node     Node
	children []*schemaNode
}

// buildSchema derives the schema tree from the model type. It is purely
// type-driven: list instances, nil optional subtrees and the values stored in a
// datastore do not affect it, so the schema is identical for every datastore and
// every device instance.
func buildSchema(root reflect.Type) (*schemaNode, error) {
	top := &schemaNode{node: Node{Kind: KindContainer}}
	if err := addSchemaChildren(top, root); err != nil {
		return nil, err
	}
	return top, nil
}

// child returns the child named name, or nil.
func (n *schemaNode) child(name string) *schemaNode {
	for _, c := range n.children {
		if c.node.Name == name {
			return c
		}
	}
	return nil
}

// addSchemaChildren appends one schema node per path-tagged field of the struct
// type t. A tag with several segments (interfaces/interface) creates the
// intermediate containers it names.
func addSchemaChildren(parent *schemaNode, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("schema: %s is not a struct", t)
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag, ok := field.Tag.Lookup("path")
		if !ok {
			continue
		}

		parts := strings.Split(tag, "/")
		owner := parent
		for _, part := range parts[:len(parts)-1] {
			existing := owner.child(part)
			if existing == nil {
				existing = &schemaNode{node: Node{Name: part, Kind: KindContainer}}
				owner.children = append(owner.children, existing)
			} else if existing.node.Kind != KindContainer {
				return fmt.Errorf("schema: %s: %q is not a container", tag, part)
			}
			owner = existing
		}

		name := parts[len(parts)-1]
		if owner.child(name) != nil {
			return fmt.Errorf("schema: duplicate node %s", tag)
		}
		child, err := newSchemaNode(field.Type)
		if err != nil {
			return fmt.Errorf("schema: %s: %w", tag, err)
		}
		child.node.Name = name
		owner.children = append(owner.children, child)
	}
	return nil
}

// newSchemaNode classifies one field type and builds its children.
func newSchemaNode(ft reflect.Type) (*schemaNode, error) {
	t := ft
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		elem := t.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem.Kind() != reflect.Struct {
			return nil, fmt.Errorf("%s is not a list of structs", t)
		}
		key := keyTagName(elem)
		if key == "" {
			return nil, fmt.Errorf("list of %s has no key field", elem)
		}
		node := &schemaNode{node: Node{Kind: KindList, Key: key}}
		if err := addSchemaChildren(node, elem); err != nil {
			return nil, err
		}
		return node, nil

	case reflect.Struct:
		node := &schemaNode{node: Node{Kind: KindContainer}}
		if err := addSchemaChildren(node, t); err != nil {
			return nil, err
		}
		return node, nil

	case reflect.Bool:
		return &schemaNode{node: Node{Kind: KindLeaf, Leaf: LeafBool}}, nil
	case reflect.Int:
		return &schemaNode{node: Node{Kind: KindLeaf, Leaf: LeafInt}}, nil
	case reflect.Uint8:
		return &schemaNode{node: Node{Kind: KindLeaf, Leaf: LeafUint8}}, nil
	case reflect.Uint16:
		return &schemaNode{node: Node{Kind: KindLeaf, Leaf: LeafUint16}}, nil
	case reflect.Uint32:
		return &schemaNode{node: Node{Kind: KindLeaf, Leaf: LeafUint32}}, nil
	case reflect.Uint64:
		return &schemaNode{node: Node{Kind: KindLeaf, Leaf: LeafUint64}}, nil
	case reflect.Float32, reflect.Float64:
		return &schemaNode{node: Node{Kind: KindLeaf, Leaf: LeafFloat64}}, nil
	case reflect.String:
		return &schemaNode{node: Node{Kind: KindLeaf, Leaf: LeafString}}, nil
	default:
		return nil, fmt.Errorf("unsupported leaf type %s", t)
	}
}

// Children returns the ordered children of the container or list entry at path,
// in the declaration order of the model fields. An empty path addresses the
// model root. Multi-segment tags are expanded, so the children of the model
// root are system-info and interfaces, and the child of interfaces is the list
// interface keyed by name.
//
// The schema is derived from the model type, so it is the same for every device
// instance: Children("interfaces/interface[name=eth0]/radio-link") returns the
// radio-link children even when that particular interface carries no radio
// link. Enforcement against the actual model instance belongs to Convert, Get
// and Set, which resolve the path against the template.
//
// Children returns ErrNotFound when path names an unknown node and
// ErrInvalidPath when path addresses a leaf or omits or misnames a list's key
// predicate.
func (r *Router) Children(path string) ([]Node, error) {
	if r.schema == nil {
		return nil, errors.New("router: schema is not built")
	}

	current := r.schema
	if path != "" {
		parsed, err := Parse(path)
		if err != nil {
			return nil, err
		}
		for i, seg := range parsed.Segments {
			child := current.child(seg.Name)
			if child == nil {
				return nil, fmt.Errorf("%w: %s", ErrNotFound, seg.Name)
			}
			switch child.node.Kind {
			case KindList:
				if seg.Key == "" {
					return nil, fmt.Errorf("%w: list %s requires a key predicate", ErrInvalidPath, seg.Name)
				}
				if seg.Key != child.node.Key {
					return nil, fmt.Errorf("%w: list %s has no key %q", ErrInvalidPath, seg.Name, seg.Key)
				}
			default:
				if seg.Key != "" {
					return nil, fmt.Errorf("%w: %s is not a list", ErrInvalidPath, seg.Name)
				}
			}
			if child.node.Kind == KindLeaf {
				if i != len(parsed.Segments)-1 {
					return nil, fmt.Errorf("%w: %s is a leaf", ErrNotFound, seg.Name)
				}
				return nil, fmt.Errorf("%w: %s is a leaf", ErrInvalidPath, path)
			}
			current = child
		}
	}

	children := make([]Node, 0, len(current.children))
	for _, c := range current.children {
		children = append(children, c.node)
	}
	return children, nil
}

// SchemaNode is one node of the static schema tree together with its children.
type SchemaNode struct {
	Node
	// Children are the node's schema children in the declaration order of the
	// model fields.
	Children []SchemaNode
}

// Schema returns the whole type-driven schema tree rooted at a synthetic
// container node whose name is empty. Like Children it is instance-agnostic: a
// list appears exactly once, with its key name and its entry children, whatever
// the datastores hold. It is what a schema dump (the CLI schema command) and a
// documentation generator walk.
func (r *Router) Schema() SchemaNode {
	if r.schema == nil {
		return SchemaNode{}
	}
	return schemaNodeOf(r.schema)
}

// schemaNodeOf converts one internal schema node into its exported form.
func schemaNodeOf(node *schemaNode) SchemaNode {
	out := SchemaNode{Node: node.node}
	if len(node.children) == 0 {
		return out
	}
	out.Children = make([]SchemaNode, 0, len(node.children))
	for _, child := range node.children {
		out.Children = append(out.Children, schemaNodeOf(child))
	}
	return out
}
