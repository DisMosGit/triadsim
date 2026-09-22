package netconf

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"

	"golang.org/x/crypto/ssh"

	"github.com/DisMosGit/triadsim/internal/netconf/notif"
	"github.com/DisMosGit/triadsim/internal/netconf/ops"
)

// session is one NETCONF session running on an SSH subsystem channel. Sessions
// share the store and the router of the server; only the transport and the
// session-id are per session. The stream is an ssh.Channel in production and an
// in-memory transcript in tests.
type session struct {
	server *Server
	id     uint32
	stream io.ReadWriter
	reader *messageReader
	writer *messageWriter
}

// serveSubsystem runs a NETCONF session on an accepted subsystem channel: the
// hello exchange, framing negotiation and then the RPC loop, until the client
// closes the session or the channel breaks.
func (s *Server) serveSubsystem(ctx context.Context, channel ssh.Channel) {
	sess := &session{
		server: s,
		id:     s.nextSessionID(),
		stream: channel,
	}
	defer s.sessionEnded(ctx, sess)

	sess.run(ctx)
}

// sessionEnded releases what one session held: its notification subscription
// (RFC 5277 §2.3) and, when it issued a confirmed commit that never got a
// confirming commit, the rollback of that commit (RFC 4741 §8.4.1).
func (s *Server) sessionEnded(ctx context.Context, sess *session) {
	if s.dispatcher != nil {
		s.dispatcher.Unsubscribe(sess.id)
	}

	// SessionEnded keeps the context values but ignores its cancellation, so
	// the rollback still runs while the server is shutting down.
	if err := s.confirmed.SessionEnded(ctx, sess.id); err != nil {
		slog.ErrorContext(ctx, "netconf: confirmed-commit rollback failed",
			"session_id", sess.id, "error", err)
	}
}

// run performs the hello exchange and then reads RPCs until the stream ends.
func (sess *session) run(ctx context.Context) {
	buffered := bufio.NewReader(sess.stream)
	sess.reader = newMessageReader(buffered, framingEOM)
	sess.writer = newMessageWriter(sess.stream, framingEOM)

	// The server hello always uses end-of-message framing: framing is
	// negotiated only after both peers have exchanged hellos (RFC 6242 §4.2).
	hello, err := marshalHello(serverHello(sess.id))
	if err != nil {
		slog.ErrorContext(ctx, "netconf: encoding hello failed", "error", err)
		return
	}
	if err := sess.writer.WriteMessage(hello); err != nil {
		slog.DebugContext(ctx, "netconf: sending hello failed", "session_id", sess.id, "error", err)
		return
	}

	// A client may already use chunked framing for its hello, so sniff it.
	mode, err := detectFraming(buffered)
	if err != nil {
		slog.DebugContext(ctx, "netconf: reading hello failed", "session_id", sess.id, "error", err)
		return
	}
	sess.reader.SetMode(mode)

	data, err := sess.reader.ReadMessage()
	if err != nil {
		slog.DebugContext(ctx, "netconf: reading hello failed", "session_id", sess.id, "error", err)
		return
	}
	client, err := parseHello(bytes.TrimSpace(data))
	if err != nil {
		slog.DebugContext(ctx, "netconf: invalid client hello", "session_id", sess.id, "error", err)
		return
	}

	negotiated := framingEOM
	if client.chunked() {
		negotiated = framingChunked
	}
	sess.reader.SetMode(negotiated)
	sess.writer.SetMode(negotiated)

	slog.DebugContext(ctx, "netconf: session started", "session_id", sess.id, "framing", negotiated.String())
	defer slog.DebugContext(ctx, "netconf: session closed", "session_id", sess.id)

	for {
		if err := ctx.Err(); err != nil {
			return
		}
		message, err := sess.reader.ReadMessage()
		if err != nil {
			sess.reportReadError(ctx, err)
			return
		}
		if !sess.handleMessage(ctx, bytes.TrimSpace(message)) {
			return
		}
	}
}

// reportReadError answers a transport-level read failure. Broken framing and an
// oversized message are malformed messages (RFC 6241 §7.1), so they are reported
// as an <rpc-error> before the session ends; an ordinary EOF is silent.
func (sess *session) reportReadError(ctx context.Context, err error) {
	switch {
	case errors.Is(err, errMessageTooBig):
		sess.writeReply(ctx, errorReply("", ops.TooBig("message exceeds %d bytes", maxMessageSize)))
	case errors.Is(err, errMalformedFrame):
		sess.writeReply(ctx, errorReply("", ops.Malformed("%v", err)))
	}

	if !errors.Is(err, io.EOF) && ctx.Err() == nil {
		slog.DebugContext(ctx, "netconf: reading message failed", "session_id", sess.id, "error", err)
	}
}

// deps returns what the operations need from the server.
func (sess *session) deps() ops.Deps {
	return ops.Deps{
		Router:    sess.server.router,
		Store:     sess.server.store,
		Bus:       sess.server.bus,
		SessionID: sess.id,
		Confirmed: sess.server.confirmed,
	}
}

// subscribe registers the session for one notification stream (RFC 5277). A
// second <create-subscription> replaces the previous subscription, and the
// subscription ends with the session.
func (sess *session) subscribe(ctx context.Context, operation *ops.Element) error {
	if sess.server.dispatcher == nil {
		return ops.NotSupported("notifications are disabled")
	}

	request, opErr := notif.ParseRequest(operation)
	if opErr != nil {
		return opErr
	}
	if err := sess.server.dispatcher.Subscribe(sess.id, request.Stream, sess.writeNotification); err != nil {
		if errors.Is(err, notif.ErrUnknownStream) {
			return ops.InvalidValue("%v", err)
		}
		return ops.Failed(err)
	}

	slog.DebugContext(ctx, "netconf: notification subscription created",
		"session_id", sess.id, "stream", request.Stream)
	return nil
}

// writeNotification writes one notification document to the session stream. It
// is the notif.Sender the dispatcher holds for this session; the message writer
// adds the negotiated framing.
func (sess *session) writeNotification(data []byte) error {
	return sess.writer.WriteMessage(data)
}
