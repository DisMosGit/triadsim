package ops

import (
	"github.com/DisMosGit/triadsim/internal/datatree"
)

// decodeXML parses a yang-data+xml body. The XML codec arrives with the media
// type negotiation; until then the format is reported as unsupported.
func decodeXML([]byte) (*Payload, *datatree.Error) {
	return nil, datatree.NotSupported("application/yang-data+xml is not implemented")
}

// encodeXML renders a read tree as a yang-data+xml document.
func encodeXML(*datatree.Node) ([]byte, *datatree.Error) {
	return nil, datatree.NotSupported("application/yang-data+xml is not implemented")
}
