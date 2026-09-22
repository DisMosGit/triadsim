package snmp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

const (
	// DefaultCommunity is the only community string the agent accepts.
	DefaultCommunity = "public"
	// maxBulkRepetitions caps the work one GetBulk repeater may cause.
	maxBulkRepetitions = 100
	// readTimeout bounds a blocking read so context cancellation is observed.
	readTimeout = 500 * time.Millisecond
)

// Options configures an Agent. Addr wins over Port when both are set; an empty
// Addr means ":Port". OnRequest, when set, is called once per accepted request
// with the lowercase operation name (get, getnext, getbulk, set).
type Options struct {
	Addr      string
	Port      int
	Community string
	OnRequest func(op string)
}

// Agent serves SNMP v2c over UDP: it answers Get, GetNext and GetBulk from the
// router's OID bindings and applies SetRequest through the router, so writes
// respect config:"false" and the model's Validate().
type Agent struct {
	router *router.Router
	opts   Options
	conn   net.PacketConn
	codec  *gosnmp.GoSNMP
}

// New returns an agent for r. Call Listen before Serve.
func New(r *router.Router, opts Options) *Agent {
	if opts.Community == "" {
		opts.Community = DefaultCommunity
	}
	if opts.Addr == "" {
		opts.Addr = fmt.Sprintf(":%d", opts.Port)
	}
	return &Agent{router: r, opts: opts}
}

// Listen binds the UDP socket. It is separate from Serve so callers can report
// a bind failure before the process blocks.
func (a *Agent) Listen() error {
	if a.router == nil {
		return errors.New("snmp: router is nil")
	}
	conn, err := net.ListenPacket("udp", a.opts.Addr)
	if err != nil {
		return fmt.Errorf("snmp: listen %s: %w", a.opts.Addr, err)
	}
	a.conn = conn
	// A single codec is shared by the single-threaded read loop; decoding
	// mutates only its internal defaults.
	a.codec = &gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: a.opts.Community}
	return nil
}

// Addr returns the bound address, or nil before Listen.
func (a *Agent) Addr() net.Addr {
	if a.conn == nil {
		return nil
	}
	return a.conn.LocalAddr()
}

// Close closes the socket and unblocks Serve.
func (a *Agent) Close() error {
	if a.conn == nil {
		return nil
	}
	return a.conn.Close()
}

// Serve reads and answers requests until the context is cancelled or the
// agent is closed.
func (a *Agent) Serve(ctx context.Context) error {
	if a.conn == nil || a.codec == nil {
		return errors.New("snmp: agent is not listening")
	}

	buf := make([]byte, 65535)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := a.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return fmt.Errorf("snmp: set read deadline: %w", err)
		}

		n, remote, err := a.conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("snmp: read: %w", err)
		}

		response, ok := a.handle(ctx, buf[:n])
		if !ok {
			continue
		}
		if _, err := a.conn.WriteTo(response, remote); err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			slog.WarnContext(ctx, "snmp: writing response failed", "error", err)
		}
	}
}

// handle decodes one datagram and returns the encoded response. The boolean is
// false when the datagram must be dropped.
func (a *Agent) handle(ctx context.Context, packet []byte) ([]byte, bool) {
	request, err := a.codec.SnmpDecodePacket(packet)
	if err != nil {
		slog.DebugContext(ctx, "snmp: dropping undecodable packet", "error", err)
		return nil, false
	}
	// SNMPv3 and any other community are out of scope: drop without a reply.
	if request.Version != gosnmp.Version2c || request.Community != a.opts.Community {
		slog.DebugContext(ctx, "snmp: dropping unauthorized packet",
			"version", request.Version, "community", request.Community)
		return nil, false
	}

	if a.opts.OnRequest != nil {
		a.opts.OnRequest(opName(request.PDUType))
	}

	var variables []gosnmp.SnmpPDU
	status, index := gosnmp.NoError, uint8(0)
	switch request.PDUType {
	case gosnmp.GetRequest:
		variables, status, index = a.get(ctx, request)
	case gosnmp.GetNextRequest:
		variables, status, index = a.getNext(ctx, request)
	case gosnmp.GetBulkRequest:
		variables, status, index = a.getBulk(ctx, request)
	case gosnmp.SetRequest:
		variables, status, index = a.set(ctx, request)
	default:
		slog.DebugContext(ctx, "snmp: unsupported PDU", "type", request.PDUType)
		return nil, false
	}

	response := &gosnmp.SnmpPacket{
		Version:    gosnmp.Version2c,
		Community:  request.Community,
		PDUType:    gosnmp.GetResponse,
		RequestID:  request.RequestID,
		Error:      status,
		ErrorIndex: index,
		Variables:  variables,
	}

	data, err := response.MarshalMsg()
	if err != nil {
		slog.WarnContext(ctx, "snmp: encoding response failed", "error", err)
		return nil, false
	}
	return data, true
}

// index snapshots the running datastore into a sorted OID index.
func (a *Agent) index(ctx context.Context) (*pduIndex, error) {
	bindings, err := a.router.Bindings(ctx, store.Running)
	if err != nil {
		return nil, err
	}
	return newPDUIndex(bindings), nil
}

func (a *Agent) get(ctx context.Context, request *gosnmp.SnmpPacket) ([]gosnmp.SnmpPDU, gosnmp.SNMPError, uint8) {
	index, err := a.index(ctx)
	if err != nil {
		return nil, gosnmp.GenErr, 0
	}

	variables := make([]gosnmp.SnmpPDU, 0, len(request.Variables))
	for _, varbind := range request.Variables {
		binding, ok := index.exact(varbind.Name)
		if !ok {
			variables = append(variables, exception(varbind.Name, gosnmp.NoSuchObject))
			continue
		}
		variables = append(variables, toPDU(binding))
	}
	return variables, gosnmp.NoError, 0
}

func (a *Agent) getNext(ctx context.Context, request *gosnmp.SnmpPacket) ([]gosnmp.SnmpPDU, gosnmp.SNMPError, uint8) {
	index, err := a.index(ctx)
	if err != nil {
		return nil, gosnmp.GenErr, 0
	}

	variables := make([]gosnmp.SnmpPDU, 0, len(request.Variables))
	for _, varbind := range request.Variables {
		binding, ok := index.next(varbind.Name)
		if !ok {
			variables = append(variables, exception(varbind.Name, gosnmp.EndOfMibView))
			continue
		}
		variables = append(variables, toPDU(binding))
	}
	return variables, gosnmp.NoError, 0
}

// getBulk follows RFC 3416 §4.2.3: the first non-repeaters varbinds get one
// successor each, then every repeater produces up to max-repetitions
// successors.
func (a *Agent) getBulk(ctx context.Context, request *gosnmp.SnmpPacket) ([]gosnmp.SnmpPDU, gosnmp.SNMPError, uint8) {
	index, err := a.index(ctx)
	if err != nil {
		return nil, gosnmp.GenErr, 0
	}

	nonRepeaters := int(request.NonRepeaters)
	if nonRepeaters > len(request.Variables) {
		nonRepeaters = len(request.Variables)
	}
	repetitions := int(request.MaxRepetitions)
	if repetitions > maxBulkRepetitions {
		repetitions = maxBulkRepetitions
	}

	variables := make([]gosnmp.SnmpPDU, 0, len(request.Variables)+repetitions)
	for _, varbind := range request.Variables[:nonRepeaters] {
		binding, ok := index.next(varbind.Name)
		if !ok {
			variables = append(variables, exception(varbind.Name, gosnmp.EndOfMibView))
			continue
		}
		variables = append(variables, toPDU(binding))
	}

	repeaters := request.Variables[nonRepeaters:]
	cursors := make([]string, len(repeaters))
	ended := make([]bool, len(repeaters))
	for i, varbind := range repeaters {
		cursors[i] = varbind.Name
	}

	for repetition := 0; repetition < repetitions; repetition++ {
		allEnded := true
		for i := range repeaters {
			if ended[i] {
				variables = append(variables, exception(cursors[i], gosnmp.EndOfMibView))
				continue
			}
			binding, ok := index.next(cursors[i])
			if !ok {
				ended[i] = true
				variables = append(variables, exception(cursors[i], gosnmp.EndOfMibView))
				continue
			}
			allEnded = false
			variables = append(variables, toPDU(binding))
			cursors[i] = binding.OID
		}
		if allEnded {
			break
		}
	}
	return variables, gosnmp.NoError, 0
}

// set validates the whole proposed running configuration before applying any
// of it, so a rejected value leaves the device untouched.
func (a *Agent) set(ctx context.Context, request *gosnmp.SnmpPacket) ([]gosnmp.SnmpPDU, gosnmp.SNMPError, uint8) {
	proposed, err := a.runningSnapshot(ctx)
	if err != nil {
		return nil, gosnmp.GenErr, 0
	}
	index, err := a.index(ctx)
	if err != nil {
		return nil, gosnmp.GenErr, 0
	}

	changed := make([]string, 0, len(request.Variables))
	for i, varbind := range request.Variables {
		// The MIB access decides, so a writable model node that is exposed
		// read-only (ifDescr, ifOperStatus) stays read-only over SNMP.
		binding, ok := index.exact(varbind.Name)
		if !ok {
			return nil, gosnmp.NoSuchName, uint8(i + 1)
		}
		if !binding.Writable {
			return nil, gosnmp.ReadOnly, uint8(i + 1)
		}
		// The binding carries the model path, so an object of a table that grew
		// at runtime — a learned MAC entry — is writable too.
		if binding.Path == "" {
			return nil, gosnmp.NoSuchName, uint8(i + 1)
		}
		value, err := fromPDU(varbind)
		if err != nil {
			return nil, gosnmp.WrongType, uint8(i + 1)
		}
		result, err := a.router.Convert(binding.Path, value)
		if err != nil {
			switch {
			case errors.Is(err, router.ErrReadOnly):
				return nil, gosnmp.ReadOnly, uint8(i + 1)
			case errors.Is(err, router.ErrTypeMismatch):
				return nil, gosnmp.WrongType, uint8(i + 1)
			default:
				return nil, gosnmp.NoSuchName, uint8(i + 1)
			}
		}
		proposed[result.Path] = result.Value
		changed = append(changed, result.Path)
	}

	if err := a.router.Validate(ctx, proposed); err != nil {
		slog.DebugContext(ctx, "snmp: set rejected by validation", "error", err)
		return nil, gosnmp.WrongValue, 1
	}

	for _, path := range changed {
		if _, err := a.router.Set(ctx, store.Running, path, proposed[path]); err != nil {
			return nil, gosnmp.GenErr, 0
		}
	}

	// Echo the written objects in their canonical SNMP representation.
	index, err = a.index(ctx)
	if err != nil {
		return nil, gosnmp.GenErr, 0
	}
	variables := make([]gosnmp.SnmpPDU, 0, len(request.Variables))
	for _, varbind := range request.Variables {
		binding, ok := index.exact(varbind.Name)
		if !ok {
			variables = append(variables, exception(varbind.Name, gosnmp.NoSuchObject))
			continue
		}
		variables = append(variables, toPDU(binding))
	}
	return variables, gosnmp.NoError, 0
}

// runningSnapshot copies the running datastore into a flat path/value map.
func (a *Agent) runningSnapshot(ctx context.Context) (map[string]any, error) {
	results, err := a.router.List(ctx, store.Running, "")
	if err != nil {
		return nil, err
	}
	values := make(map[string]any, len(results))
	for _, result := range results {
		values[result.Path] = result.Value
	}
	return values, nil
}

// opName is the metric label for a PDU type.
func opName(pduType gosnmp.PDUType) string {
	switch pduType {
	case gosnmp.GetRequest:
		return "get"
	case gosnmp.GetNextRequest:
		return "getnext"
	case gosnmp.GetBulkRequest:
		return "getbulk"
	case gosnmp.SetRequest:
		return "set"
	default:
		return "other"
	}
}
