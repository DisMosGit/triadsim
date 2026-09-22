package netconf

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/DisMosGit/triadsim/internal/netconf/ops"
)

// incomingRPC is a parsed <rpc>. Body keeps the raw inner XML, so every
// operation parses exactly the payload it expects.
type incomingRPC struct {
	XMLName   xml.Name
	MessageID string `xml:"message-id,attr"`
	Body      []byte `xml:",innerxml"`
}

// rpcError is one <rpc-error> of a reply.
type rpcError struct {
	Type     string `xml:"error-type"`
	Tag      string `xml:"error-tag"`
	Severity string `xml:"error-severity"`
	Message  string `xml:"error-message,omitempty"`
	Path     string `xml:"error-path,omitempty"`
}

// rpcReply is a NETCONF <rpc-reply>: exactly one of ok, data or a list of
// errors.
type rpcReply struct {
	XMLName   xml.Name
	MessageID string       `xml:"message-id,attr"`
	Ok        *struct{}    `xml:"ok"`
	Data      *ops.Element `xml:"data"`
	Errors    []rpcError   `xml:"rpc-error"`
}

// newReply starts a reply that echoes the request's message-id.
func newReply(messageID string) rpcReply {
	return rpcReply{
		XMLName:   xml.Name{Space: BaseNamespace, Local: "rpc-reply"},
		MessageID: messageID,
	}
}

// okReply is the positive reply of an operation with no data.
func okReply(messageID string) rpcReply {
	reply := newReply(messageID)
	reply.Ok = &struct{}{}
	return reply
}

// errorReply is a reply carrying exactly one error.
func errorReply(messageID string, err *ops.Error) rpcReply {
	reply := newReply(messageID)
	reply.Errors = []rpcError{{
		Type:     err.Type,
		Tag:      err.Tag,
		Severity: err.Severity,
		Message:  err.Message,
		Path:     err.Path,
	}}
	return reply
}

// errorReplyFrom turns an operation error into a reply. An error that is not an
// ops.Error is reported as application/operation-failed, so a device failure is
// never mistaken for a protocol error.
func errorReplyFrom(messageID string, err error) rpcReply {
	opErr := &ops.Error{
		Type:     ops.TypeApplication,
		Tag:      ops.TagOperationFailed,
		Severity: ops.SeverityError,
		Message:  err.Error(),
	}
	_ = errors.As(err, &opErr)
	return errorReply(messageID, opErr)
}

// marshal encodes the reply as one XML document.
func (r rpcReply) marshal() ([]byte, error) {
	data, err := xml.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("netconf: encode rpc-reply: %w", err)
	}
	return data, nil
}

// parseRPC decodes one <rpc> envelope and checks the attributes the base
// protocol requires.
func parseRPC(data []byte) (incomingRPC, *ops.Error) {
	var rpc incomingRPC
	if err := xml.Unmarshal(data, &rpc); err != nil {
		return incomingRPC{}, ops.Malformed("cannot parse rpc: %v", err)
	}
	if rpc.XMLName.Local != "rpc" {
		return incomingRPC{}, ops.Malformed("expected rpc, got %q", rpc.XMLName.Local)
	}
	if strings.TrimSpace(rpc.MessageID) == "" {
		return incomingRPC{}, ops.MissingAttribute("message-id")
	}
	return rpc, nil
}

// dispatch runs the operation of one RPC and returns its reply. The boolean
// reports that the session must end after the reply is written, which is what
// close-session and a malformed payload require.
func (sess *session) dispatch(ctx context.Context, rpc incomingRPC) (rpcReply, bool) {
	operation, err := ops.ParseElement(rpc.Body)
	if err != nil {
		return errorReply(rpc.MessageID, ops.Malformed("cannot parse rpc body: %v", err)), true
	}

	switch operation.Name {
	case "get-config":
		data, err := ops.GetConfig(ctx, sess.deps(), operation)
		if err != nil {
			return errorReplyFrom(rpc.MessageID, err), false
		}
		reply := newReply(rpc.MessageID)
		reply.Data = data
		return reply, false

	case "edit-config":
		if err := ops.EditConfig(ctx, sess.deps(), operation); err != nil {
			return errorReplyFrom(rpc.MessageID, err), false
		}
		return okReply(rpc.MessageID), false

	case "commit":
		if err := ops.Commit(ctx, sess.deps(), operation); err != nil {
			return errorReplyFrom(rpc.MessageID, err), false
		}
		return okReply(rpc.MessageID), false

	case "discard-changes":
		if err := ops.DiscardChanges(ctx, sess.deps(), operation); err != nil {
			return errorReplyFrom(rpc.MessageID, err), false
		}
		return okReply(rpc.MessageID), false

	case "create-subscription":
		if err := sess.subscribe(ctx, operation); err != nil {
			return errorReplyFrom(rpc.MessageID, err), false
		}
		return okReply(rpc.MessageID), false

	case "close-session":
		return okReply(rpc.MessageID), true

	default:
		return errorReply(rpc.MessageID, ops.NotSupported("operation %s is not supported", operation.Name)), false
	}
}

// handleMessage decodes, dispatches and answers one RPC. It reports whether the
// session may continue.
func (sess *session) handleMessage(ctx context.Context, message []byte) bool {
	rpc, parseErr := parseRPC(message)
	if parseErr != nil {
		// A malformed message has no reliable message-id, so the reply carries
		// an empty one and the session ends (RFC 6241 §7.1).
		sess.writeReply(ctx, errorReply("", parseErr))
		return false
	}

	reply, closeSession := sess.dispatch(ctx, rpc)
	sess.writeReply(ctx, reply)
	return !closeSession
}

// writeReply encodes and writes one reply.
func (sess *session) writeReply(ctx context.Context, reply rpcReply) {
	data, err := reply.marshal()
	if err != nil {
		slog.ErrorContext(ctx, "netconf: encoding reply failed", "session_id", sess.id, "error", err)
		return
	}
	if err := sess.writer.WriteMessage(data); err != nil {
		slog.DebugContext(ctx, "netconf: writing reply failed", "session_id", sess.id, "error", err)
	}
}
