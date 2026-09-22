package snmp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Vendor trap OIDs. An SNMPv2-Trap-PDU carries the trap type in its
// snmpTrapOID.0 varbind (RFC 3416 §4.2.6).
const (
	TrapRadioLinkDown = router.EnterpriseOID + ".0.1" // simRadioLinkDown
	TrapRadioLinkUp   = router.EnterpriseOID + ".0.2" // simRadioLinkUp
	TrapSyncHoldover  = router.EnterpriseOID + ".0.3" // simSyncHoldover
	TrapSyncRestored  = router.EnterpriseOID + ".0.4" // simSyncRestored
	TrapL2Storm       = router.EnterpriseOID + ".0.5" // simL2StormDetected
	TrapConfigChanged = router.EnterpriseOID + ".0.6" // simConfigChanged
)

// Mandatory varbinds and MIB objects the trap payload uses.
const (
	sysUpTimeOID   = "1.3.6.1.2.1.1.3.0"
	snmpTrapOIDOID = "1.3.6.1.2.1.11.4.1.0"
	// trapIfDescrBase is the ifDescr column; trap varbinds index it with the
	// interface index of the link.
	trapIfDescrBase = "1.3.6.1.2.1.2.2.1.2"
)

// TrapOptions configures a TrapSender. Addr wins over Host and Port when both
// are set; an empty Host means the loopback address.
type TrapOptions struct {
	// Addr is the trap destination as host:port, for example "127.0.0.1:1162".
	Addr string
	// Host and Port build the destination as Host:Port when Addr is empty.
	Host string
	Port int
	// Community is the SNMPv2c community. Empty selects DefaultCommunity.
	Community string
	// Router resolves the varbind payload of a trap. It is optional: without
	// one, traps carry only sysUpTime.0, snmpTrapOID.0 and the event text.
	Router *router.Router
	// Bus is the event source. A nil bus makes Run a no-op.
	Bus *event.Bus
	// Clock is the injected time source; it is what anchor sysUpTime. A nil
	// clock means clock.RealClock{}.
	Clock clock.Clock
}

// TrapSender turns the alarms and state transitions of the event bus into
// SNMPv2-Trap-PDUs and sends them to one destination. Delivery is
// fire-and-forget: an unreachable receiver is logged and dropped, which is
// consistent with the unconfirmed nature of an SNMPv2 trap.
type TrapSender struct {
	router    *router.Router
	bus       *event.Bus
	clock     clock.Clock
	start     time.Time
	community string
	dest      string

	conn net.Conn
	// requests numbers the trap PDUs the sender emits. A trap carries no
	// request to correlate with, but RFC 3416 requires the field.
	requests atomic.Uint32
}

// NewTrapSender returns a sender for the configured destination. Call Listen
// before Run or Send.
func NewTrapSender(opts TrapOptions) *TrapSender {
	if opts.Community == "" {
		opts.Community = DefaultCommunity
	}
	if opts.Clock == nil {
		opts.Clock = clock.RealClock{}
	}
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	dest := opts.Addr
	if dest == "" {
		dest = net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))
	}
	return &TrapSender{
		router:    opts.Router,
		bus:       opts.Bus,
		clock:     opts.Clock,
		start:     opts.Clock.Now(),
		community: opts.Community,
		dest:      dest,
	}
}

// Listen resolves the destination and opens the sending socket, so a bad
// address is reported at startup instead of when the first alarm fires.
func (s *TrapSender) Listen() error {
	if _, err := net.ResolveUDPAddr("udp", s.dest); err != nil {
		return fmt.Errorf("snmp: resolve trap destination %s: %w", s.dest, err)
	}
	conn, err := net.Dial("udp", s.dest)
	if err != nil {
		return fmt.Errorf("snmp: trap destination %s: %w", s.dest, err)
	}
	s.conn = conn
	return nil
}

// Destination returns the trap destination the sender was built for.
func (s *TrapSender) Destination() string { return s.dest }

// Close closes the sending socket.
func (s *TrapSender) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// Run sends a trap for every event of the bus until ctx is cancelled. It
// returns immediately when no bus is configured.
func (s *TrapSender) Run(ctx context.Context) {
	if s.bus == nil {
		return
	}
	events := s.bus.Subscribe()
	defer s.bus.Unsubscribe(events)

	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			if err := s.Send(ctx, e); err != nil {
				slog.WarnContext(ctx, "snmp: sending trap failed", "error", err, "type", e.Type, "resource", e.Resource)
			}
		}
	}
}

// Send builds and sends the trap an event maps to. An event that is not a trap
// is a no-op, not an error.
func (s *TrapSender) Send(ctx context.Context, e event.Event) error {
	oid := trapFor(e)
	if oid == "" {
		return nil
	}
	if s.conn == nil {
		return errors.New("snmp: trap sender is not listening")
	}

	packet := &gosnmp.SnmpPacket{
		Version:   gosnmp.Version2c,
		Community: s.community,
		PDUType:   gosnmp.SNMPv2Trap,
		RequestID: s.requests.Add(1),
		Variables: s.varbinds(ctx, oid, e),
	}
	data, err := packet.MarshalMsg()
	if err != nil {
		return fmt.Errorf("snmp: encode trap %s: %w", oid, err)
	}
	if _, err := s.conn.Write(data); err != nil {
		return fmt.Errorf("snmp: send trap %s: %w", oid, err)
	}
	return nil
}

// trapFor returns the trap OID an event maps to, or an empty string when the
// event is not a trap. An alarm keeps its name across the raised and the
// cleared event; the event type selects the trap.
func trapFor(e event.Event) string {
	switch e.Type {
	case event.TypeAlarmRaised:
		switch e.Alarm {
		case event.AlarmRadioLinkDown:
			return TrapRadioLinkDown
		case event.AlarmL2Storm:
			return TrapL2Storm
		}
	case event.TypeAlarmCleared:
		if e.Alarm == event.AlarmRadioLinkDown {
			return TrapRadioLinkUp
		}
	case event.TypeStateTransition:
		if e.Domain != event.DomainSync {
			return ""
		}
		switch {
		case strings.HasPrefix(e.To, "holdover"):
			return TrapSyncHoldover
		case strings.HasPrefix(e.From, "holdover") && e.To == "locked":
			return TrapSyncRestored
		}
	case event.TypeConfigChanged:
		return TrapConfigChanged
	}
	return ""
}

// varbinds builds the variable binding list of one trap: the two mandatory
// varbinds followed by the payload the trap type carries.
func (s *TrapSender) varbinds(ctx context.Context, oid string, e event.Event) []gosnmp.SnmpPDU {
	varbinds := []gosnmp.SnmpPDU{
		{Name: sysUpTimeOID, Type: gosnmp.TimeTicks, Value: s.uptimeTicks()},
		{Name: snmpTrapOIDOID, Type: gosnmp.ObjectIdentifier, Value: oid},
	}

	switch oid {
	case TrapRadioLinkDown, TrapRadioLinkUp:
		varbinds = append(varbinds, s.radioVarbinds(ctx, e.Resource)...)
	case TrapSyncHoldover, TrapSyncRestored:
		varbinds = append(varbinds, s.pathVarbinds(ctx, "ptp/clock/state")...)
	}
	return varbinds
}

// radioVarbinds returns the payload of a radio trap: the received level, the
// fade margin and the interface description of the failing link.
func (s *TrapSender) radioVarbinds(ctx context.Context, link string) []gosnmp.SnmpPDU {
	if s.router == nil || link == "" {
		return nil
	}

	var varbinds []gosnmp.SnmpPDU
	for _, suffix := range []string{"rssi", "fade-margin"} {
		path := "interfaces/interface[name=" + link + "]/radio-link/" + suffix
		if binding, ok := s.binding(ctx, path); ok {
			varbinds = append(varbinds, toPDU(binding))
		}
	}

	if index := s.interfaceIndex(ctx, link); index > 0 {
		varbinds = append(varbinds, gosnmp.SnmpPDU{
			Name:  trapIfDescrBase + "." + strconv.Itoa(index),
			Type:  gosnmp.OctetString,
			Value: []byte(link),
		})
	}
	return varbinds
}

// pathVarbinds returns one exposed model leaf as a varbind.
func (s *TrapSender) pathVarbinds(ctx context.Context, path string) []gosnmp.SnmpPDU {
	binding, ok := s.binding(ctx, path)
	if !ok {
		return nil
	}
	return []gosnmp.SnmpPDU{toPDU(binding)}
}

// binding reads one exposed model path as an SNMP binding, so the OID table
// decides the type and the value conversion of the varbind.
func (s *TrapSender) binding(ctx context.Context, path string) (router.Binding, bool) {
	if s.router == nil {
		return router.Binding{}, false
	}
	result, err := s.router.Get(ctx, store.Running, path)
	if err != nil || result.OID == "" {
		return router.Binding{}, false
	}
	return router.Binding{
		OID:      result.OID,
		Path:     result.Path,
		Value:    result.Value,
		Type:     result.Type,
		Writable: result.Writable,
	}, true
}

// interfaceIndex returns the 1-based interface index of a link, which is the
// instance sub-identifier of its ifDescr varbind.
func (s *TrapSender) interfaceIndex(ctx context.Context, link string) int {
	if s.router == nil {
		return 0
	}
	device, err := s.router.Snapshot(ctx, store.Running)
	if err != nil {
		return 0
	}
	for i := range device.Interfaces {
		if device.Interfaces[i].Name == link {
			return i + 1
		}
	}
	return 0
}

// uptimeTicks returns sysUpTime as hundredths of a second since the sender was
// built.
func (s *TrapSender) uptimeTicks() uint32 {
	elapsed := s.clock.Now().Sub(s.start).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return uint32(elapsed * 100)
}
