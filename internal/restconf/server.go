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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/restconf/ops"
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
	// maxBodyBytes caps a request body.
	maxBodyBytes = 1 << 20
	// allowDataMethods is the Allow header of the data resource.
	allowDataMethods = "GET, HEAD, PUT, PATCH, POST, DELETE"
)

// StormSimulator injects a broadcast storm on one port. It is satisfied by the
// L2 domain; a nil StormSimulator answers with operation-not-supported.
type StormSimulator interface {
	Storm(ctx context.Context, port string, packets uint32) error
}

// SyncSimulator injects the loss of a synchronization source. It is satisfied
// by the sync domain; a nil SyncSimulator answers with operation-not-supported.
type SyncSimulator interface {
	SyncLoss(ctx context.Context, source string) (string, error)
}

// RadioSimulator injects a radio-link failure and its restore. It is satisfied
// by the radio domain; a nil RadioSimulator answers with
// operation-not-supported.
type RadioSimulator interface {
	// RadioFailure injects a fade into a link and returns its new link state.
	RadioFailure(ctx context.Context, link string, fadeDB float64) (string, error)
	// RadioRestore clears the injected fade of a link and returns its state.
	RadioRestore(ctx context.Context, link string) (string, error)
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
	// Sync receives the synchronization simulation endpoint's requests.
	Sync SyncSimulator
	// Radio receives the radio-link simulation endpoint's requests.
	Radio RadioSimulator
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
	mux.HandleFunc("/api/simulate/sync-loss", s.handleSyncLoss)
	mux.HandleFunc("/api/simulate/radio-failure", s.handleRadioFailure)
	mux.HandleFunc("/api/simulate/radio-restore", s.handleRadioRestore)

	return mux
}

// deps builds the operation dependencies of the server.
func (s *Server) deps() ops.Deps {
	return ops.Deps{Router: s.router, Store: s.store, Bus: s.bus}
}

// handleData implements the /restconf/data resource: it resolves the target and
// dispatches by HTTP method.
func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	target, httpErr := s.parseTarget(r)
	if httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.handleGet(w, r, target)
	case http.MethodPut:
		s.handleWrite(w, r, target, http.MethodPut)
	case http.MethodPatch:
		s.handleWrite(w, r, target, http.MethodPatch)
	case http.MethodPost:
		s.handleWrite(w, r, target, http.MethodPost)
	case http.MethodDelete:
		s.handleDelete(w, r, target)
	default:
		s.writeError(w, r, methodNotAllowed(allowDataMethods))
	}
}

// handleGet answers GET and HEAD.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, target *target) {
	format, httpErr := responseFormat(r.Header.Get("Accept"))
	if httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}

	node, opErr := ops.Get(r.Context(), s.deps(), target.Datastore, target.Path, target.Content)
	if opErr != nil {
		s.writeTreeError(w, r, opErr)
		return
	}

	body, opErr := ops.Encode(format, node)
	if opErr != nil {
		s.writeTreeError(w, r, opErr)
		return
	}

	w.Header().Set("Content-Type", mediaTypeOf(format))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// handleWrite answers PUT, PATCH and POST.
func (s *Server) handleWrite(w http.ResponseWriter, r *http.Request, target *target, method string) {
	if httpErr := checkWritable(target, method); httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}

	payload, httpErr := s.decodeBody(w, r)
	if httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}

	switch method {
	case http.MethodPut:
		created := s.resourceMissing(r, target)
		if opErr := ops.Put(r.Context(), s.deps(), target.Datastore, opsTarget(target), payload); opErr != nil {
			s.writeTreeError(w, r, opErr)
			return
		}
		if created {
			w.Header().Set("Location", locationFor(target.Path))
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusOK)

	case http.MethodPatch:
		if opErr := ops.Patch(r.Context(), s.deps(), target.Datastore, opsTarget(target), payload); opErr != nil {
			s.writeTreeError(w, r, opErr)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case http.MethodPost:
		created, opErr := ops.Post(r.Context(), s.deps(), target.Datastore, opsTarget(target), payload)
		if opErr != nil {
			s.writeTreeError(w, r, opErr)
			return
		}
		w.Header().Set("Location", locationFor(created))
		w.WriteHeader(http.StatusCreated)
	}
}

// handleDelete answers DELETE.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, target *target) {
	if httpErr := checkWritable(target, http.MethodDelete); httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}
	if opErr := ops.Delete(r.Context(), s.deps(), target.Datastore, opsTarget(target)); opErr != nil {
		s.writeTreeError(w, r, opErr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkWritable rejects a write to a datastore or resource that is read-only.
func checkWritable(target *target, method string) *httpError {
	if target.Datastore == store.Startup {
		return methodNotAllowed("GET, HEAD")
	}
	if method == http.MethodPut && target.Collection {
		return methodNotAllowed("GET, HEAD, PATCH, POST, DELETE")
	}
	if method == http.MethodPost && !target.Collection {
		return methodNotAllowed("GET, HEAD, PUT, PATCH, DELETE")
	}
	return nil
}

// resourceMissing reports whether the target resource holds no data yet, which
// turns a PUT into a 201 Created instead of a 200 OK.
func (s *Server) resourceMissing(r *http.Request, target *target) bool {
	_, opErr := ops.Get(r.Context(), s.deps(), target.Datastore, target.Path, datatree.ContentAll)
	return opErr != nil && opErr.Tag == datatree.TagDataMissing
}

// decodeBody reads and decodes the request body in its declared media type.
func (s *Server) decodeBody(w http.ResponseWriter, r *http.Request) (*ops.Payload, *httpError) {
	format, httpErr := requestFormat(r.Header.Get("Content-Type"))
	if httpErr != nil {
		return nil, httpErr
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, tooLarge("request body exceeds the %d byte limit", maxBodyBytes)
		}
		return nil, malformedRequest("reading the request body: %v", err)
	}

	payload, opErr := ops.Decode(format, body)
	if opErr != nil {
		return nil, dataTreeHTTPError(opErr)
	}
	return payload, nil
}

// opsTarget converts a parsed target into the operation package's target.
func opsTarget(target *target) ops.Target {
	return ops.Target{
		Path:       target.Path,
		BasePath:   target.BasePath,
		Name:       target.Name,
		Schema:     target.Node,
		Key:        target.Segment.Key,
		KeyValue:   target.Segment.Value,
		Collection: target.Collection,
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
	if s.opts.Storm == nil {
		s.writeError(w, r, notImplemented(r.URL.Path))
		return
	}

	var request struct {
		Port    string `json:"port"`
		Packets uint32 `json:"packets"`
	}
	if httpErr := s.decodeJSON(w, r, &request); httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}
	if request.Port == "" {
		s.writeError(w, r, invalidValue("port must not be empty"))
		return
	}

	if err := s.opts.Storm.Storm(r.Context(), request.Port, request.Packets); err != nil {
		s.writeError(w, r, newHTTPError(http.StatusUnprocessableEntity, errorTypeProtocol, "invalid-value", "%v", err))
		return
	}

	w.Header().Set("Content-Type", MediaTypeJSON)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "port": request.Port, "packets": request.Packets})
}

// handleSyncLoss implements POST /api/simulate/sync-loss: it drives the PTP
// clock into holdover because its synchronization source was lost. An empty
// port means the loss is not attributed to one interface.
func (s *Server) handleSyncLoss(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, methodNotAllowed(http.MethodPost))
		return
	}
	if s.opts.Sync == nil {
		s.writeError(w, r, notImplemented(r.URL.Path))
		return
	}

	var request struct {
		Port string `json:"port"`
	}
	if httpErr := s.decodeJSON(w, r, &request); httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}

	state, err := s.opts.Sync.SyncLoss(r.Context(), request.Port)
	if err != nil {
		s.writeError(w, r, newHTTPError(http.StatusUnprocessableEntity, errorTypeProtocol, "invalid-value", "%v", err))
		return
	}

	w.Header().Set("Content-Type", MediaTypeJSON)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "port": request.Port, "state": state})
}

// handleRadioFailure implements POST /api/simulate/radio-failure: it injects a
// fade into a radio link, so the link budget, the fade margin and the
// radioLinkDown/radioLinkDegraded alarms reflect a failing link. An empty link
// selects the device's first radio link and an absent or zero fade-db the
// domain's default failure depth.
func (s *Server) handleRadioFailure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, methodNotAllowed(http.MethodPost))
		return
	}
	if s.opts.Radio == nil {
		s.writeError(w, r, notImplemented(r.URL.Path))
		return
	}

	var request struct {
		Link   string  `json:"link"`
		FadeDB float64 `json:"fade-db"`
	}
	if httpErr := s.decodeJSON(w, r, &request); httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}
	if request.FadeDB < 0 {
		s.writeError(w, r, invalidValue("fade-db must not be negative"))
		return
	}

	state, err := s.opts.Radio.RadioFailure(r.Context(), request.Link, request.FadeDB)
	if err != nil {
		s.writeError(w, r, newHTTPError(http.StatusUnprocessableEntity, errorTypeProtocol, "invalid-value", "%v", err))
		return
	}

	w.Header().Set("Content-Type", MediaTypeJSON)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "link": request.Link, "state": state})
}

// handleRadioRestore implements POST /api/simulate/radio-restore: it clears the
// fade a simulation injected into a radio link.
func (s *Server) handleRadioRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, r, methodNotAllowed(http.MethodPost))
		return
	}
	if s.opts.Radio == nil {
		s.writeError(w, r, notImplemented(r.URL.Path))
		return
	}

	var request struct {
		Link string `json:"link"`
	}
	if httpErr := s.decodeJSON(w, r, &request); httpErr != nil {
		s.writeError(w, r, httpErr)
		return
	}

	state, err := s.opts.Radio.RadioRestore(r.Context(), request.Link)
	if err != nil {
		s.writeError(w, r, newHTTPError(http.StatusUnprocessableEntity, errorTypeProtocol, "invalid-value", "%v", err))
		return
	}

	w.Header().Set("Content-Type", MediaTypeJSON)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "link": request.Link, "state": state})
}

// decodeJSON reads a simulation request body into out, applying the shared body
// limit and reporting the shared error documents.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, out any) *httpError {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(out)
	if err == nil {
		return nil
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return tooLarge("request body exceeds the %d byte limit", maxBodyBytes)
	}
	return malformedRequest("malformed request body: %v", err)
}

// mediaTypeOf returns the content type of a format.
func mediaTypeOf(format ops.Format) string {
	if format == ops.FormatXML {
		return MediaTypeXML
	}
	return MediaTypeJSON
}

// requestFormat maps a Content-Type header to a codec format. A missing header
// defaults to JSON, which keeps plain curl usable.
func requestFormat(contentType string) (ops.Format, *httpError) {
	switch strings.TrimSpace(strings.Split(contentType, ";")[0]) {
	case "", MediaTypeJSON:
		return ops.FormatJSON, nil
	case MediaTypeXML:
		return ops.FormatXML, nil
	default:
		return "", unsupportedMedia("Content-Type %q is not supported", contentType)
	}
}

// responseFormat maps an Accept header to a codec format.
func responseFormat(accept string) (ops.Format, *httpError) {
	switch {
	case accept == "", strings.Contains(accept, "*/*"), strings.Contains(accept, MediaTypeJSON):
		return ops.FormatJSON, nil
	case strings.Contains(accept, MediaTypeXML):
		return ops.FormatXML, nil
	default:
		return "", notAcceptable("Accept %q is not supported", accept)
	}
}
