package netconf

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// SubsystemName is the SSH subsystem a NETCONF client asks for.
const SubsystemName = "netconf"

// Options configures a Server. Addr wins over Port when both are set; an empty
// Addr means ":Port".
type Options struct {
	// Addr is the TCP listen address, for example "127.0.0.1:0" in tests.
	Addr string
	// Port builds the listen address as ":Port" when Addr is empty.
	Port int
	// HostKey is the SSH host key. A nil HostKey makes Listen generate an
	// ephemeral ed25519 key, which is enough for a simulator: the key is not
	// persisted, so clients need to accept a new host key after a restart.
	HostKey ssh.Signer
}

// subsystemRequest is the payload of an SSH "subsystem" request (RFC 4254 §6.5).
type subsystemRequest struct {
	Name string
}

// Server serves NETCONF over SSH. It owns one TCP listener and one goroutine
// per connection; each SSH session that requests the netconf subsystem runs its
// own NETCONF session with its own session-id over the same store.
type Server struct {
	router *router.Router
	store  store.Store
	bus    *event.Bus
	opts   Options

	mu       sync.Mutex
	listener net.Listener
	config   *ssh.ServerConfig
	conns    map[net.Conn]struct{}
	closed   bool

	sessions atomic.Uint32
	wg       sync.WaitGroup
}

// New returns a server for r, st and bus. bus may be nil, which only disables
// ConfigChanged publication. Call Listen before Serve.
func New(r *router.Router, st store.Store, bus *event.Bus, opts Options) *Server {
	if opts.Addr == "" {
		opts.Addr = fmt.Sprintf(":%d", opts.Port)
	}
	return &Server{
		router: r,
		store:  st,
		bus:    bus,
		opts:   opts,
		conns:  make(map[net.Conn]struct{}),
	}
}

// Listen builds the SSH server configuration and binds the TCP socket. It is
// separate from Serve so callers can report a bind failure before the process
// blocks.
func (s *Server) Listen() error {
	if s.router == nil {
		return errors.New("netconf: router is nil")
	}
	if s.store == nil {
		return errors.New("netconf: store is nil")
	}

	config, err := s.serverConfig()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("netconf: listen %s: %w", s.opts.Addr, err)
	}

	s.mu.Lock()
	s.config = config
	s.listener = listener
	s.mu.Unlock()
	return nil
}

// serverConfig builds the SSH server configuration. Every authentication method
// accepts any client: the simulator has no authentication by design.
func (s *Server) serverConfig() (*ssh.ServerConfig, error) {
	signer := s.opts.HostKey
	if signer == nil {
		_, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("netconf: generate host key: %w", err)
		}
		signer, err = ssh.NewSignerFromKey(private)
		if err != nil {
			return nil, fmt.Errorf("netconf: host key: %w", err)
		}
	}

	accept := func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }
	config := &ssh.ServerConfig{
		// No auth anywhere: any login, password, key or keyboard-interactive
		// exchange is accepted, so `ssh -s netconf user@host` just works.
		NoClientAuth:                true,
		PasswordCallback:            accept,
		PublicKeyCallback:           func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil },
		KeyboardInteractiveCallback: func(ssh.ConnMetadata, ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) { return nil, nil },
	}
	config.AddHostKey(signer)
	return config, nil
}

// Addr returns the bound address, or nil before Listen.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Close stops accepting connections and closes every open one, which unblocks
// Serve. It is idempotent.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	for conn := range s.conns {
		_ = conn.Close()
	}
	return err
}

// Serve accepts and serves connections until the context is cancelled or the
// server is closed. Both are clean shutdowns and return nil.
func (s *Server) Serve(ctx context.Context) error {
	if s.listener == nil || s.config == nil {
		return errors.New("netconf: server is not listening")
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-stop:
		}
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			return fmt.Errorf("netconf: accept: %w", err)
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn)
		}()
	}

	s.wg.Wait()
	return nil
}

// handleConn runs the SSH handshake and serves the session channels of one
// connection until the client disconnects or the server closes.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	s.track(conn, true)
	defer s.track(conn, false)
	defer func() { _ = conn.Close() }()

	sshConn, channels, requests, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		// Port scanners and probes are expected; a failed handshake is debug
		// noise, not an error.
		slog.DebugContext(ctx, "netconf: ssh handshake failed", "remote", conn.RemoteAddr(), "error", err)
		return
	}
	defer func() { _ = sshConn.Close() }()

	slog.DebugContext(ctx, "netconf: ssh client connected",
		"remote", sshConn.RemoteAddr(), "user", sshConn.User())

	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			slog.DebugContext(ctx, "netconf: accepting channel failed", "error", err)
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleSession(ctx, channel, channelRequests)
		}()
	}
}

// handleSession accepts the netconf subsystem on one SSH session channel and
// runs the NETCONF protocol on it. Every other request is rejected.
func (s *Server) handleSession(ctx context.Context, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer func() { _ = channel.Close() }()

	for req := range requests {
		if req.Type != "subsystem" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}

		var payload subsystemRequest
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil || payload.Name != SubsystemName {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)

		// The NETCONF session owns the channel from here on. Keep answering
		// further requests so a client that waits for a reply cannot hang.
		go func() {
			for req := range requests {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
			}
		}()

		s.serveSubsystem(ctx, channel)
		return
	}
}

// nextSessionID returns the id of the next NETCONF session. Ids start at 1 and
// are never reused within a run.
func (s *Server) nextSessionID() uint32 {
	for {
		if id := s.sessions.Add(1); id != 0 {
			return id
		}
	}
}

// track records an open connection so Close can unblock it.
func (s *Server) track(conn net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if add {
		s.conns[conn] = struct{}{}
		return
	}
	delete(s.conns, conn)
}
