package netconf

import (
	"encoding/xml"
	"fmt"
)

// BaseNamespace is the namespace of every NETCONF protocol element.
const BaseNamespace = "urn:ietf:params:xml:ns:netconf:base:1.0"

// Capability URNs.
const (
	// CapabilityBase10 is the base protocol capability from RFC 6241.
	CapabilityBase10 = "urn:ietf:params:netconf:base:1.0"
	// CapabilityBase11 adds chunked framing (RFC 6242 §4.2).
	CapabilityBase11 = "urn:ietf:params:netconf:base:1.1"
	// CapabilityCandidate adds the candidate datastore, commit and
	// discard-changes.
	CapabilityCandidate = "urn:ietf:params:netconf:capability:candidate:1.0"
	// CapabilityWritableRunning allows edit-config to target running directly.
	CapabilityWritableRunning = "urn:ietf:params:netconf:capability:writable-running:1.0"
)

// Capabilities returns the capabilities the server advertises, in a stable
// order. The server only advertises what it implements: confirmed-commit and
// notifications are added with their implementation.
func Capabilities() []string {
	return []string{
		CapabilityBase11,
		CapabilityBase10,
		CapabilityCandidate,
		CapabilityWritableRunning,
	}
}

// hello is the NETCONF <hello> element as sent by the server.
type hello struct {
	XMLName      xml.Name
	Capabilities capabilities `xml:"capabilities"`
	SessionID    uint32       `xml:"session-id"`
}

// capabilities is the <capabilities> container of a <hello>.
type capabilities struct {
	Items []string `xml:"capability"`
}

// helloMessage is a received <hello>. Parsing matches the local element names
// only, so a client that omits or rewrites the namespace still interoperates.
type helloMessage struct {
	XMLName      xml.Name
	Capabilities []string `xml:"capabilities>capability"`
	SessionID    uint32   `xml:"session-id"`
}

// serverHello builds the hello the server sends at the start of a session.
func serverHello(sessionID uint32) hello {
	return hello{
		XMLName:      xml.Name{Space: BaseNamespace, Local: "hello"},
		Capabilities: capabilities{Items: Capabilities()},
		SessionID:    sessionID,
	}
}

// parseHello decodes a client hello and checks that it announces the base
// protocol, which every NETCONF client must.
func parseHello(data []byte) (helloMessage, error) {
	var message helloMessage
	if err := xml.Unmarshal(data, &message); err != nil {
		return helloMessage{}, fmt.Errorf("netconf: parse hello: %w", err)
	}
	if message.XMLName.Local != "hello" {
		return helloMessage{}, fmt.Errorf("netconf: expected hello, got %q", message.XMLName.Local)
	}
	if !message.hasCapability(CapabilityBase10) && !message.hasCapability(CapabilityBase11) {
		return helloMessage{}, fmt.Errorf("netconf: hello does not advertise a base capability")
	}
	return message, nil
}

// hasCapability reports whether the hello advertised urn.
func (h helloMessage) hasCapability(urn string) bool {
	for _, capability := range h.Capabilities {
		if capability == urn {
			return true
		}
	}
	return false
}

// chunked reports whether the session must use chunked framing. Both peers have
// to advertise base 1.1 for that; this server always advertises it, so the
// client's hello decides.
func (h helloMessage) chunked() bool {
	return h.hasCapability(CapabilityBase11)
}

// marshalHello encodes a hello with the base namespace on the root element.
func marshalHello(h hello) ([]byte, error) {
	data, err := xml.Marshal(h)
	if err != nil {
		return nil, fmt.Errorf("netconf: encode hello: %w", err)
	}
	return data, nil
}
