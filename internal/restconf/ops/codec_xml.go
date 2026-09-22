package ops

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
)

// restconfNamespace is the ietf-restconf module, used when a read of the whole
// datastore is wrapped in a <data> element.
const restconfNamespace = "urn:ietf:params:xml:ns:yang:ietf-restconf"

// decodeXML parses a yang-data+xml body: the root element is the resource and
// its children are the document's children. Module namespaces are matched by
// local name, because the simulator addresses the data tree by router paths.
func decodeXML(data []byte) (*Payload, *datatree.Error) {
	document, opErr := parseXMLDocument(data)
	if opErr != nil {
		return nil, opErr
	}
	return &Payload{Name: document.Name, Documents: []*datatree.Document{document}}, nil
}

// parseXMLDocument parses one XML document into a data-tree document.
func parseXMLDocument(data []byte) (*datatree.Document, *datatree.Error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, datatree.Malformed("the request body is empty")
	}

	decoder := xml.NewDecoder(bytes.NewReader(trimmed))
	var root *datatree.Document
	var stack []*datatree.Document

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, datatree.Malformed("malformed XML body: %v", err)
		}

		switch token := token.(type) {
		case xml.StartElement:
			if root != nil && len(stack) == 0 {
				return nil, datatree.Malformed("the request body must hold exactly one XML document")
			}
			element := &datatree.Document{Name: token.Name.Local}
			if len(stack) == 0 {
				root = element
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, element)
			}
			stack = append(stack, element)

		case xml.EndElement:
			if len(stack) == 0 {
				return nil, datatree.Malformed("unexpected end element %s", token.Name.Local)
			}
			stack = stack[:len(stack)-1]

		case xml.CharData:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.Text += string(token)
				top.HasText = strings.TrimSpace(top.Text) != ""
			}
		}
	}

	if root == nil {
		return nil, datatree.Malformed("the request body holds no XML element")
	}
	return root, nil
}

// encodeXML renders a read tree as a yang-data+xml document. The addressed node
// is the document element and declares its module namespace; nested nodes
// declare theirs only when the module changes.
func encodeXML(node *datatree.Node) ([]byte, *datatree.Error) {
	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)

	if node.Name == "" {
		// The datastore root has several top-level nodes, so it is wrapped in
		// the ietf-restconf <data> element to stay well-formed.
		buffer.WriteString(`<data xmlns="` + restconfNamespace + `">`)
		currentNS := restconfNamespace
		for _, child := range node.Children {
			currentNS = writeXMLNode(&buffer, child, currentNS)
		}
		buffer.WriteString(`</data>`)
		return buffer.Bytes(), nil
	}

	writeXMLNode(&buffer, node, "")
	return buffer.Bytes(), nil
}

// writeXMLNode writes one node and returns the namespace in scope afterwards.
// A list writes one element per entry, without a wrapper of its own.
func writeXMLNode(buffer *bytes.Buffer, node *datatree.Node, currentNS string) string {
	if node.Kind == router.KindList {
		for _, entry := range node.Children {
			currentNS = writeXMLNode(buffer, entry, currentNS)
		}
		return currentNS
	}

	namespace := router.ModuleFor(node.Path).Namespace
	writeXMLStart(buffer, node.Name, namespace, currentNS)
	if namespace != currentNS {
		currentNS = namespace
	}

	switch node.Kind {
	case router.KindLeaf:
		writeXMLText(buffer, datatree.FormatLeaf(node.Value))
	case router.KindContainer:
		for _, child := range node.Children {
			currentNS = writeXMLNode(buffer, child, currentNS)
		}
	}

	writeXMLEnd(buffer, node.Name)
	return currentNS
}

// writeXMLStart writes an opening tag, declaring the namespace when it changes.
func writeXMLStart(buffer *bytes.Buffer, name, namespace, currentNS string) {
	buffer.WriteByte('<')
	buffer.WriteString(name)
	if namespace != currentNS {
		buffer.WriteString(` xmlns="`)
		writeXMLText(buffer, namespace)
		buffer.WriteByte('"')
	}
	buffer.WriteByte('>')
}

// writeXMLEnd writes a closing tag.
func writeXMLEnd(buffer *bytes.Buffer, name string) {
	buffer.WriteString("</")
	buffer.WriteString(name)
	buffer.WriteByte('>')
}

// writeXMLText writes XML-escaped character data.
func writeXMLText(buffer *bytes.Buffer, text string) {
	_ = xml.EscapeText(buffer, []byte(text))
}
