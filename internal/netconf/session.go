package netconf

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"

	"golang.org/x/crypto/ssh"
)

// session is one NETCONF session running on an SSH subsystem channel. Sessions
// share the store and the router of the server; only the transport and the
// session-id are per session.
type session struct {
	server  *Server
	id      uint32
	channel ssh.Channel
	reader  *messageReader
	writer  *messageWriter
}

// serveSubsystem runs a NETCONF session on an accepted subsystem channel: the
// hello exchange, framing negotiation and then the RPC loop, until the client
// closes the session or the channel breaks.
func (s *Server) serveSubsystem(ctx context.Context, channel ssh.Channel) {
	sess := &session{
		server:  s,
		id:      s.nextSessionID(),
		channel: channel,
	}
	sess.run(ctx)
}

// run performs the hello exchange and then reads RPCs until the channel ends.
func (sess *session) run(ctx context.Context) {
	buffered := bufio.NewReader(sess.channel)
	sess.reader = newMessageReader(buffered, framingEOM)
	sess.writer = newMessageWriter(sess.channel, framingEOM)

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
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				slog.DebugContext(ctx, "netconf: reading message failed", "session_id", sess.id, "error", err)
			}
			return
		}
		sess.handleMessage(ctx, bytes.TrimSpace(message))
	}
}

// handleMessage processes one message. RPC decoding, dispatch and replies arrive
// with the next step of the phase; until then a framed message is only
// acknowledged in the debug log, which keeps the framing testable on its own.
func (sess *session) handleMessage(ctx context.Context, message []byte) {
	slog.DebugContext(ctx, "netconf: message received", "session_id", sess.id, "bytes", len(message))
}
