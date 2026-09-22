package notif

import (
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/netconf/ops"
)

// NotificationNamespace is the RFC 5277 namespace of <notification>,
// <eventTime> and <create-subscription>.
const NotificationNamespace = "urn:ietf:params:xml:ns:netconf:notification:1.0"

// EventsNamespace is the module namespace of the <event> payload inside a
// notification. It is the same module the RESTCONF event stream exposes.
const EventsNamespace = "urn:sim:sim-events"

// StreamName is the only event stream the simulator exposes. A
// <create-subscription> without <stream> selects it.
const StreamName = "sim-events"

// ErrUnknownStream means the client asked for a stream the simulator does not
// have.
var ErrUnknownStream = errors.New("netconf: unknown event stream")

// Request is a parsed <create-subscription>.
type Request struct {
	// Stream is the stream the client asked for. An absent <stream> means
	// StreamName.
	Stream string
}

// ParseRequest validates the payload of <create-subscription>. Filtering
// (<filter>) and replay (<startTime>, <stopTime>) are not implemented, so they
// are rejected instead of being silently ignored.
func ParseRequest(operation *ops.Element) (Request, *ops.Error) {
	if operation.Child("filter") != nil {
		return Request{}, ops.NotSupported("create-subscription filter is not supported")
	}
	for _, name := range []string{"startTime", "stopTime"} {
		if operation.Child(name) != nil {
			return Request{}, ops.NotSupported("create-subscription replay (%s) is not supported", name)
		}
	}
	for _, child := range operation.Children {
		switch child.Name {
		case "stream", "filter", "startTime", "stopTime":
		default:
			return Request{}, ops.UnknownElement(child.Name)
		}
	}

	request := Request{Stream: StreamName}
	if stream := operation.Child("stream"); stream != nil {
		request.Stream = stream.TrimmedText()
		if request.Stream == "" {
			return Request{}, ops.InvalidValue("create-subscription stream is empty")
		}
	}
	if request.Stream != StreamName {
		return Request{}, ops.InvalidValue("unknown event stream %q", request.Stream)
	}
	return request, nil
}

// notification is one RFC 5277 <notification> document.
type notification struct {
	XMLName   xml.Name
	EventTime string `xml:"eventTime"`
	Event     eventPayload
}

// eventPayload is the simulator's event as the notification content.
type eventPayload struct {
	XMLName  xml.Name
	Type     string `xml:"type"`
	Resource string `xml:"resource,omitempty"`
	Severity string `xml:"severity,omitempty"`
	Message  string `xml:"message,omitempty"`
}

// Render encodes one bus event as a complete <notification> document:
//
//	<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
//	  <eventTime>2024-01-02T03:04:05Z</eventTime>
//	  <event xmlns="urn:sim:sim-events">
//	    <type>AlarmRaised</type><resource>radio0</resource>
//	  </event>
//	</notification>
//
// The timestamp must be set; the event bus stamps events that arrive without
// one. The document is one NETCONF message and is framed by the session.
func Render(e event.Event) ([]byte, error) {
	doc := notification{
		XMLName:   xml.Name{Space: NotificationNamespace, Local: "notification"},
		EventTime: e.Timestamp.UTC().Format(time.RFC3339Nano),
		Event: eventPayload{
			XMLName:  xml.Name{Space: EventsNamespace, Local: "event"},
			Type:     string(e.Type),
			Resource: e.Resource,
			Severity: e.Severity,
			Message:  e.Message,
		},
	}

	data, err := xml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("netconf: encode notification: %w", err)
	}
	return data, nil
}

// Sender writes one complete notification document to a session stream. The
// session's message writer adds the negotiated framing.
type Sender func(data []byte) error

// Subscription is one active subscription: the notifications of one stream
// queued for one session.
type Subscription struct {
	sessionID uint32
	stream    string
	send      Sender

	out  chan []byte
	done chan struct{}
	once sync.Once
}

// newSubscription returns a subscription whose writer goroutine starts at once.
func newSubscription(sessionID uint32, stream string, send Sender) *Subscription {
	sub := &Subscription{
		sessionID: sessionID,
		stream:    stream,
		send:      send,
		out:       make(chan []byte, event.DefaultBuffer),
		done:      make(chan struct{}),
	}
	go sub.writeLoop()
	return sub
}

// deliver queues one notification. A session that does not read its
// notifications quickly enough loses them and the drop is logged, so one slow
// client can neither stall the dispatcher nor delay the other sessions.
func (s *Subscription) deliver(data []byte) {
	select {
	case s.out <- data:
	default:
		slog.Warn("netconf: notification dropped, session is not reading",
			"session_id", s.sessionID, "stream", s.stream, "buffer", event.DefaultBuffer)
	}
}

// writeLoop writes queued notifications until the subscription stops or the
// session stream breaks.
func (s *Subscription) writeLoop() {
	for {
		select {
		case data := <-s.out:
			if err := s.send(data); err != nil {
				slog.Debug("netconf: sending notification failed",
					"session_id", s.sessionID, "error", err)
				return
			}
		case <-s.done:
			return
		}
	}
}

// stop ends the subscription. It does not wait for the writer: the writer may
// be blocked on a session that stopped reading, and neither the session's own
// cleanup nor the server shutdown may block on that.
func (s *Subscription) stop() {
	s.once.Do(func() { close(s.done) })
}
