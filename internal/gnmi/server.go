package gnmi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// shutdownTimeout bounds the graceful gRPC shutdown when the context is
// cancelled.
const shutdownTimeout = 2 * time.Second

// Organization is the organization the capability models are published under.
const Organization = "TriadSim"

// ModelVersion is the semantic version the capability models advertise.
const ModelVersion = "0.1.0"

// Options configures a Server. Addr wins over Port when both are set; an empty
// Addr means ":Port".
type Options struct {
	// Addr is the TCP listen address, for example "127.0.0.1:0" in tests.
	Addr string
	// Port builds the listen address as ":Port" when Addr is empty.
	Port int
	// Clock stamps read notifications. A nil Clock means clock.RealClock{}.
	Clock clock.Clock
}

// Server serves gNMI over gRPC. It owns one TCP listener and, like the other
// planes, reads and writes the shared store.
type Server struct {
	router *router.Router
	store  store.Store
	bus    *event.Bus
	clock  clock.Clock
	opts   Options

	listener net.Listener
	grpc     *grpc.Server
}

// New returns a server for r, st and bus. bus may be nil, which only disables
// STREAM subscriptions. Call Listen before Serve.
func New(r *router.Router, st store.Store, bus *event.Bus, opts Options) *Server {
	if opts.Addr == "" {
		opts.Addr = fmt.Sprintf(":%d", opts.Port)
	}
	if opts.Clock == nil {
		opts.Clock = clock.RealClock{}
	}
	return &Server{router: r, store: st, bus: bus, clock: opts.Clock, opts: opts}
}

// Listen binds the TCP socket and registers the gNMI service. It is separate
// from Serve so callers can report a bind failure before the process blocks.
func (s *Server) Listen() error {
	if s.router == nil {
		return errors.New("gnmi: router is nil")
	}
	if s.store == nil {
		return errors.New("gnmi: store is nil")
	}

	listener, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("gnmi: listen %s: %w", s.opts.Addr, err)
	}

	s.listener = listener
	s.grpc = grpc.NewServer()
	gnmi.RegisterGNMIServer(s.grpc, &service{server: s})
	return nil
}

// Addr returns the bound address, or nil before Listen.
func (s *Server) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Close closes the listener. Serve returns nil once it observes the close.
func (s *Server) Close() error {
	if s.listener == nil {
		return nil
	}
	return s.listener.Close()
}

// Serve answers RPCs until the context is cancelled or the server is closed,
// then stops gracefully within shutdownTimeout.
func (s *Server) Serve(ctx context.Context) error {
	if s.grpc == nil {
		return errors.New("gnmi: server is not listening")
	}

	done := make(chan error, 1)
	go func() { done <- s.grpc.Serve(s.listener) }()

	select {
	case <-ctx.Done():
		s.stop()
		return nil
	case err := <-done:
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, grpc.ErrServerStopped) {
			return fmt.Errorf("gnmi: serve: %w", err)
		}
		return nil
	}
}

// stop stops the gRPC server, forcing it down when the graceful path does not
// finish in time.
func (s *Server) stop() {
	stopped := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(shutdownTimeout):
		s.grpc.Stop()
	}
}

// service adapts the Server to the generated gNMI service interface.
type service struct {
	gnmi.UnimplementedGNMIServer

	server *Server
}

// Capabilities reports the simulator's models and the encodings the service
// serves.
func (s *service) Capabilities(context.Context, *gnmi.CapabilityRequest) (*gnmi.CapabilityResponse, error) {
	models := make([]*gnmi.ModelData, 0, len(modules))
	for _, module := range modules {
		models = append(models, &gnmi.ModelData{
			Name:         module.Name,
			Organization: Organization,
			Version:      ModelVersion,
		})
	}

	return &gnmi.CapabilityResponse{
		SupportedModels: models,
		SupportedEncodings: []gnmi.Encoding{
			gnmi.Encoding_JSON,
			gnmi.Encoding_JSON_IETF,
			gnmi.Encoding_PROTO,
		},
	}, nil
}
