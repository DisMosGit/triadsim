package netconf

import (
	"context"
	"log/slog"

	"golang.org/x/crypto/ssh"
)

// serveSubsystem runs the NETCONF protocol on an accepted subsystem channel.
//
// The hello exchange, framing negotiation and RPC loop arrive with the next
// steps of the phase; for now the session simply closes.
func (s *Server) serveSubsystem(ctx context.Context, channel ssh.Channel) {
	slog.DebugContext(ctx, "netconf: subsystem started", "session_id", s.nextSessionID())
	_ = channel.CloseWrite()
}
