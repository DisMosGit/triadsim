// Package restconf implements the RESTCONF management plane on top of chi:
// URL parsing, JSON and XML codecs for the yang-data media types, and the
// GET/PUT/PATCH/POST/DELETE operations against the store.
//
// There is no authentication. The package is HTTP and wire formats only: the
// datastore operations live in internal/restconf/ops and the shared subtree
// semantics in internal/datatree.
package restconf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// RESTCONF root and media types (RFC 8040 §11.3).
const (
	// BasePath is the RESTCONF API root.
	BasePath = "/restconf"
	// MediaTypeJSON is application/yang-data+json, the default response type.
	MediaTypeJSON = "application/yang-data+json"
	// MediaTypeXML is application/yang-data+xml.
	MediaTypeXML = "application/yang-data+xml"

	// shutdownTimeout bounds the HTTP shutdown when the context is cancelled.
	shutdownTimeout = 2 * time.Second
	// readHeaderTimeout bounds reading a request header.
	readHeaderTimeout = 5 * time.Second
)

// StormSimulator injects a broadcast storm on one port. It is satisfied by the
// L2 domain; a nil StormSimulator answers with operation-not-supported.
type StormSimulator interface {
	Storm(ctx context.Context, port string, packets uint32) error
}

// Options configures a Server. Addr wins over Port when both are set; an empty
// Addr means ":Port".
type Options struct {
	// Addr is the TCP listen address, for example "127.0.0.1:0" in tests.
	Addr string
	// Port builds the listen address as ":Port" when Addr is empty.
	Port int
	// Storm receives the simulation endpoint's requests.
	Storm StormSimulator
}

// Server serves RESTCONF over HTTP. It owns one TCP listener; every request is
// stateless and reads and writes the shared store.
type Server struct {
	router *router.Router
	store  store.Store
	bus    *event.Bus
	opts   Options

	listener net.Listener
	http     *http.Server
}

// New returns a server for r, st and bus. bus may be nil, which only disables
// ConfigChanged publication. Call Listen before Serve.
func New(r *router.Router, st store.Store, bus *event.Bus, opts Options) *Server {
	if opts.Addr == "" {
		opts.Addr = fmt.Sprintf(":%d", opts.Port)
	}
	return &Server{router: r, store: st, bus: bus, opts: opts}
}

// Listen binds the TCP socket. It is separate from Serve so callers can report
// a bind failure before the process blocks.
func (s *Server) Listen() error {
	if s.router == nil {
		return errors.New("restconf: router is nil")
	}
	if s.store == nil {
		return errors.New("restconf: store is nil")
	}

	listener, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("restconf: listen %s: %w", s.opts.Addr, err)
	}
	s.listener = listener
	s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: readHeaderTimeout}
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

// Serve answers requests until the context is cancelled or the server is
// closed, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context) error {
	if s.http == nil {
		return errors.New("restconf: server is not listening")
	}

	done := make(chan error, 1)
	go func() { done <- s.http.Serve(s.listener) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
		return nil
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return fmt.Errorf("restconf: serve: %w", err)
	}
}

// Handler returns the HTTP handler, exposed so tests can serve it directly.
func (s *Server) Handler() http.Handler {
	mux := chi.NewRouter()

	mux.Route("/restconf", func(mux chi.Router) {
		mux.HandleFunc("/data", s.handleData)
		mux.HandleFunc("/data/*", s.handleData)
		mux.HandleFunc("/operations", s.handleNotImplemented)
		mux.HandleFunc("/operations/*", s.handleNotImplemented)
		mux.HandleFunc("/streams", s.handleNotImplemented)
		mux.HandleFunc("/streams/*", s.handleNotImplemented)
	})
	mux.HandleFunc("/api/simulate/l2-storm", s.handleStorm)

	return mux
}

// handleData implements the /restconf/data resource: it resolves the target and
// dispatches by HTTP method.
func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	if _, httpErr := s.parseTarget(r); httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.writeError(w, r, notImplemented("GET"))
	case http.MethodPut:
		s.writeError(w, r, notImplemented("PUT"))
	case http.MethodPatch:
		s.writeError(w, r, notImplemented("PATCH"))
	case http.MethodPost:
		s.writeError(w, r, notImplemented("POST"))
	case http.MethodDelete:
		s.writeError(w, r, notImplemented("DELETE"))
	default:
		s.writeError(w, r, methodNotAllowed("GET, HEAD, PUT, PATCH, POST, DELETE"))
	}
}

// handleNotImplemented answers the operation and stream resources the simulator
// does not implement yet.
func (s *Server) handleNotImplemented(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, notImplemented(r.URL.Path))
}

// handleStorm implements POST /api/simulate/l2-storm.
func (s *Server) handleStorm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, methodNotAllowed(http.MethodPost))
		return
	}
	s.writeError(w, r, notImplemented(r.URL.Path))
}
