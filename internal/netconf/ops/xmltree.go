package ops

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Element is one element of a NETCONF XML document. Namespaces are matched by
// local name on input, because the data tree of the simulator is addressed by
// router paths, not by YANG module identity. On output Space is set on the top
// element of a module subtree only, so its children inherit the namespace.
type Element struct {
	Space    string
	Name     string
	Attrs    []xml.Attr
	Children []*Element
	Text     string
}

// Element builds a child element. Children are added with Append.
func NewElement(name string) *Element {
	return &Element{Name: name}
}

// Append adds children and returns the receiver, so trees can be built in one
// expression.
func (e *Element) Append(children ...*Element) *Element {
	e.Children = append(e.Children, children...)
	return e
}

// Child returns the first child named name, or nil.
func (e *Element) Child(name string) *Element {
	for _, child := range e.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

// ChildrenNamed returns every child named name.
func (e *Element) ChildrenNamed(name string) []*Element {
	var children []*Element
	for _, child := range e.Children {
		if child.Name == name {
			children = append(children, child)
		}
	}
	return children
}

// Attr returns the value of the attribute whose local name is name. Matching by
// local name accepts both operation="merge" and nc:operation="merge".
func (e *Element) Attr(name string) (string, bool) {
	for _, attr := range e.Attrs {
		if attr.Name.Local == name {
			return attr.Value, true
		}
	}
	return "", false
}

// TrimmedText returns the element's character data without surrounding
// whitespace, which is how leaf values are read from a data tree.
func (e *Element) TrimmedText() string {
	return strings.TrimSpace(e.Text)
}

// ParseElement parses one XML document into an element tree. Spaces and
// comments are ignored; the document must have exactly one root element.
func ParseElement(data []byte) (*Element, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var root *Element
	var stack []*Element
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := token.(type) {
		case xml.StartElement:
			if root != nil && len(stack) == 0 {
				return nil, fmt.Errorf("netconf: multiple root elements")
			}
			element := &Element{Space: t.Name.Space, Name: t.Name.Local, Attrs: t.Attr}
			if len(stack) == 0 {
				root = element
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, element)
			}
			stack = append(stack, element)

		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("netconf: unexpected end element %s", t.Name.Local)
			}
			stack = stack[:len(stack)-1]

		case xml.CharData:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.Text += string(t)
			}
		}
	}

	if root == nil {
		return nil, fmt.Errorf("netconf: empty document")
	}
	return root, nil
}

// MarshalXML encodes the element tree. Elements without children and without
// text are written as empty elements, which is what <ok/> and empty <data/>
// need.
func (e *Element) MarshalXML(encoder *xml.Encoder, _ xml.StartElement) error {
	start := xml.StartElement{
		Name: xml.Name{Space: e.Space, Local: e.Name},
		Attr: e.Attrs,
	}
	if err := encoder.EncodeToken(start); err != nil {
		return err
	}
	if e.Text != "" {
		if err := encoder.EncodeToken(xml.CharData(e.Text)); err != nil {
			return err
		}
	}
	for _, child := range e.Children {
		if err := encoder.Encode(child); err != nil {
			return err
		}
	}
	return encoder.EncodeToken(start.End())
}
