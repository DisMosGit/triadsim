package netconf

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"

	"golang.org/x/crypto/ssh"

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
	sess.run(ctx)
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
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				slog.DebugContext(ctx, "netconf: reading message failed", "session_id", sess.id, "error", err)
			}
			return
		}
		if !sess.handleMessage(ctx, bytes.TrimSpace(message)) {
			return
		}
	}
}

// deps returns what the operations need from the server.
func (sess *session) deps() ops.Deps {
	return ops.Deps{
		Router: sess.server.router,
		Store:  sess.server.store,
		Bus:    sess.server.bus,
	}
}
