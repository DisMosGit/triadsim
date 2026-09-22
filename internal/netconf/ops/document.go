package ops

import (
	"strings"

	"github.com/DisMosGit/triadsim/internal/datatree"
)

// documentOf converts a parsed XML element into the protocol-neutral document
// the data-tree engine edits. Attributes are keyed by local name, so both
// operation= and nc:operation= are found.
func documentOf(element *Element) *datatree.Document {
	document := &datatree.Document{
		Name:    element.Name,
		Text:    element.Text,
		HasText: strings.TrimSpace(element.Text) != "",
	}
	if len(element.Attrs) > 0 {
		document.Attrs = make(map[string]string, len(element.Attrs))
		for _, attr := range element.Attrs {
			document.Attrs[attr.Name.Local] = attr.Value
		}
	}
	for _, child := range element.Children {
		document.Children = append(document.Children, documentOf(child))
	}
	return document
}
