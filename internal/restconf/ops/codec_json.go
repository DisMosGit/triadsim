package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
)

// Decode parses a request body in the given format.
func Decode(format Format, data []byte) (*Payload, *datatree.Error) {
	switch format {
	case FormatJSON:
		return decodeJSON(data)
	case FormatXML:
		return decodeXML(data)
	default:
		return nil, datatree.NotSupported("unsupported media type")
	}
}

// Encode renders the result of a read in the given format.
func Encode(format Format, node *datatree.Node) ([]byte, *datatree.Error) {
	switch format {
	case FormatJSON:
		return encodeJSON(node)
	case FormatXML:
		return encodeXML(node)
	default:
		return nil, datatree.NotSupported("unsupported media type")
	}
}

// decodeJSON parses a yang-data+json body (RFC 8040 §6.1). The body is a single
// member object: the member name is the resource with an optional module
// prefix, and the value is an object (container or list entry), an array (a
// list) or a scalar (a leaf).
func decodeJSON(data []byte) (*Payload, *datatree.Error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, datatree.Malformed("the request body is empty")
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, datatree.Malformed("malformed JSON body: %v", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, datatree.Malformed("the request body must hold exactly one JSON document")
	}

	object, ok := root.(map[string]any)
	if !ok {
		return nil, datatree.InvalidValue("the request body must be a JSON object")
	}
	if len(object) != 1 {
		return nil, datatree.InvalidValue("the request body must have exactly one member")
	}

	for name, value := range object {
		resource := stripModule(name)
		switch value := value.(type) {
		case map[string]any:
			return &Payload{Name: resource, Documents: []*datatree.Document{documentOfMap(resource, value)}}, nil
		case []any:
			documents := make([]*datatree.Document, 0, len(value))
			for _, entry := range value {
				entryMap, ok := entry.(map[string]any)
				if !ok {
					return nil, datatree.InvalidValue("list entries of %s must be JSON objects", resource)
				}
				documents = append(documents, documentOfMap(resource, entryMap))
			}
			return &Payload{Name: resource, Documents: documents}, nil
		default:
			return &Payload{Name: resource, Scalar: scalarText(value), IsScalar: true}, nil
		}
	}
	return nil, datatree.InvalidValue("the request body must have exactly one member")
}

// documentOfMap converts a JSON object into a document. Keys are visited in
// sorted order so the same body always produces the same document.
func documentOfMap(name string, values map[string]any) *datatree.Document {
	document := &datatree.Document{Name: name}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		childName := stripModule(key)
		switch value := values[key].(type) {
		case map[string]any:
			document.Children = append(document.Children, documentOfMap(childName, value))
		case []any:
			for _, entry := range value {
				if entryMap, ok := entry.(map[string]any); ok {
					document.Children = append(document.Children, documentOfMap(childName, entryMap))
					continue
				}
				document.Children = append(document.Children, leafOf(childName, scalarText(entry)))
			}
		case nil:
			document.Children = append(document.Children, leafOf(childName, ""))
		default:
			document.Children = append(document.Children, leafOf(childName, scalarText(value)))
		}
	}
	return document
}

// leafOf builds a leaf document.
func leafOf(name, text string) *datatree.Document {
	return &datatree.Document{Name: name, Text: text, HasText: true}
}

// scalarText renders a decoded JSON scalar as the text a leaf carries.
func scalarText(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	case bool:
		return strconv.FormatBool(value)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	case nil:
		return ""
	default:
		return ""
	}
}

// stripModule removes the "module:" prefix a RESTCONF JSON name may carry.
func stripModule(name string) string {
	if index := strings.IndexByte(name, ':'); index >= 0 {
		return name[index+1:]
	}
	return name
}

// encodeJSON renders a read tree as a yang-data+json document. The top member
// is module-qualified; nested members are not, as RFC 8040 §6.1 requires.
func encodeJSON(node *datatree.Node) ([]byte, *datatree.Error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')

	if node.Name == "" {
		// The datastore root: one module-qualified member per top-level node.
		for i, child := range node.Children {
			if i > 0 {
				buffer.WriteByte(',')
			}
			writeJSONKey(&buffer, moduleQualified(child))
			writeJSONValue(&buffer, child)
		}
	} else {
		writeJSONKey(&buffer, moduleQualified(node))
		writeJSONValue(&buffer, node)
	}

	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// writeJSONValue writes one node as JSON: a scalar for a leaf, an object for a
// container or list entry, an array for a list.
func writeJSONValue(buffer *bytes.Buffer, node *datatree.Node) {
	switch node.Kind {
	case router.KindLeaf:
		encoded, err := json.Marshal(node.Value)
		if err != nil {
			encoded = []byte("null")
		}
		buffer.Write(encoded)

	case router.KindList:
		buffer.WriteByte('[')
		for i, entry := range node.Children {
			if i > 0 {
				buffer.WriteByte(',')
			}
			writeJSONValue(buffer, entry)
		}
		buffer.WriteByte(']')

	default:
		buffer.WriteByte('{')
		for i, child := range node.Children {
			if i > 0 {
				buffer.WriteByte(',')
			}
			writeJSONKey(buffer, child.Name)
			writeJSONValue(buffer, child)
		}
		buffer.WriteByte('}')
	}
}

// writeJSONKey writes one JSON object member name.
func writeJSONKey(buffer *bytes.Buffer, name string) {
	encoded, err := json.Marshal(name)
	if err != nil {
		encoded = []byte(`""`)
	}
	buffer.Write(encoded)
	buffer.WriteByte(':')
}

// moduleQualified returns the "module:name" key of a top-level node.
func moduleQualified(node *datatree.Node) string {
	module := router.ModuleFor(node.Path)
	if module.Name == "" {
		return node.Name
	}
	return module.Name + ":" + node.Name
}
