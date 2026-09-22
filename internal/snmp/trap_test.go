package snmp

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
)

// trapReceiver is a UDP socket that collects the trap datagrams of a test.
type trapReceiver struct {
	conn net.PacketConn
	addr *net.UDPAddr
}

// newTrapReceiver binds an ephemeral loopback port.
func newTrapReceiver(t *testing.T) *trapReceiver {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &trapReceiver{conn: conn, addr: conn.LocalAddr().(*net.UDPAddr)}
}

// receive reads one trap datagram and decodes it.
func (r *trapReceiver) receive(t *testing.T) *gosnmp.SnmpPacket {
	t.Helper()

	require.NoError(t, r.conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 65535)
	n, _, err := r.conn.ReadFrom(buf)
	require.NoError(t, err)
	return decodeTrap(t, buf[:n])
}

// decodeTrap decodes one trap datagram.
func decodeTrap(t *testing.T, data []byte) *gosnmp.SnmpPacket {
	t.Helper()

	packet, err := (&gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: DefaultCommunity}).SnmpDecodePacket(data)
	require.NoError(t, err)
	return packet
}

// silent asserts that no datagram arrives within a short window.
func (r *trapReceiver) silent(t *testing.T) {
	t.Helper()

	require.NoError(t, r.conn.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	buf := make([]byte, 1024)
	_, _, err := r.conn.ReadFrom(buf)
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "expected no trap, got %v", err)
}

// trapFixture bundles a listening sender with its receiver and fake clock.
type trapFixture struct {
	sender   *TrapSender
	receiver *trapReceiver
	bus      *event.Bus
	clock    *clock.FakeClock
}

// newTrapFixture builds a sender over the seeded device and starts listening.
func newTrapFixture(t *testing.T, bus *event.Bus) *trapFixture {
	t.Helper()

	receiver := newTrapReceiver(t)
	clk := clock.NewFakeClock()
	sender := NewTrapSender(TrapOptions{
		Addr:   receiver.addr.String(),
		Router: newTestRouter(t),
		Bus:    bus,
		Clock:  clk,
	})
	require.NoError(t, sender.Listen())
	t.Cleanup(func() { require.NoError(t, sender.Close()) })

	return &trapFixture{sender: sender, receiver: receiver, bus: bus, clock: clk}
}

// Wire constants taken from the specification, never from the implementation:
// a test that looks a varbind up by the production constant cannot notice the
// constant drifting away from the MIB.
const (
	// SNMPv2-MIB::sysUpTime.0 (RFC 3418), the first mandatory varbind.
	wantSysUpTimeOID = "1.3.6.1.2.1.1.3.0"
	// SNMPv2-MIB::snmpTrapOID.0, the second mandatory varbind of an
	// SNMPv2-Trap-PDU (RFC 3418 snmpTrapOID ::= { snmpTrap 1 },
	// snmpTrap ::= { snmpMIBObjects 4 }; RFC 3416 §4.2.6).
	wantSnmpTrapOIDOID = "1.3.6.1.6.3.1.1.4.1.0"
)

// varbind returns the varbind with the given OID, ignoring the leading dot
// gosnmp's decoder adds.
func varbind(t *testing.T, packet *gosnmp.SnmpPacket, oid string) gosnmp.SnmpPDU {
	t.Helper()

	for _, v := range packet.Variables {
		if strings.TrimPrefix(v.Name, ".") == oid {
			return v
		}
	}
	require.Failf(t, "varbind not found", "OID %s in %v", oid, packet.Variables)
	return gosnmp.SnmpPDU{}
}

// trapOID returns the trap type of a decoded PDU, without the leading dot the
// decoder adds to an ObjectIdentifier.
func trapOID(t *testing.T, packet *gosnmp.SnmpPacket) string {
	t.Helper()

	value, ok := varbind(t, packet, wantSnmpTrapOIDOID).Value.(string)
	require.True(t, ok, "snmpTrapOID.0 is an ObjectIdentifier")
	return strings.TrimPrefix(value, ".")
}

func TestSendRadioLinkDownTrap(t *testing.T) {
	f := newTrapFixture(t, nil)
	f.clock.Advance(90 * time.Second)

	require.NoError(t, f.sender.Send(t.Context(), event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "radio0",
		Severity: "critical",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	}))

	packet := f.receiver.receive(t)
	assert.Equal(t, gosnmp.Version2c, packet.Version)
	assert.Equal(t, DefaultCommunity, packet.Community)
	assert.Equal(t, gosnmp.SNMPv2Trap, packet.PDUType)

	// The two mandatory varbinds come first, in the order RFC 3416 §4.2.6
	// prescribes: sysUpTime.0 (TimeTicks), then snmpTrapOID.0.
	require.GreaterOrEqual(t, len(packet.Variables), 2)
	first, second := packet.Variables[0], packet.Variables[1]
	assert.Equal(t, wantSysUpTimeOID, strings.TrimPrefix(first.Name, "."))
	assert.Equal(t, gosnmp.TimeTicks, first.Type)
	assert.Equal(t, uint32(9000), first.Value)
	assert.Equal(t, wantSnmpTrapOIDOID, strings.TrimPrefix(second.Name, "."))
	assert.Equal(t, gosnmp.ObjectIdentifier, second.Type)
	assert.Equal(t, TrapRadioLinkDown, trapOID(t, packet))

	// The payload names the link and its measured levels. The seeded RSSI OID
	// is a vendor OpaqueDouble, the interface description a standard string.
	assert.Contains(t, varbind(t, packet, "1.3.6.1.4.1.99999.1.1.1.0").Name, "1.3.6.1.4.1.99999.1.1.1.0")
	assert.Contains(t, varbind(t, packet, "1.3.6.1.4.1.99999.1.1.2.0").Name, "1.3.6.1.4.1.99999.1.1.2.0")

	descr := varbind(t, packet, "1.3.6.1.2.1.2.2.1.2.1")
	switch value := descr.Value.(type) {
	case string:
		assert.Equal(t, "radio0", value)
	case []byte:
		assert.Equal(t, "radio0", string(value))
	default:
		t.Fatalf("unexpected ifDescr value type %T", descr.Value)
	}
}

func TestSendRadioLinkUpTrap(t *testing.T) {
	f := newTrapFixture(t, nil)

	require.NoError(t, f.sender.Send(t.Context(), event.Event{
		Type:     event.TypeAlarmCleared,
		Resource: "radio0",
		Severity: "cleared",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	}))

	assert.Equal(t, TrapRadioLinkUp, trapOID(t, f.receiver.receive(t)))
}

func TestSendSyncTrap(t *testing.T) {
	f := newTrapFixture(t, nil)

	require.NoError(t, f.sender.Send(t.Context(), event.Event{
		Type:     event.TypeStateTransition,
		Resource: "ptp/clock",
		Domain:   event.DomainSync,
		From:     "locked",
		To:       "holdover-in-spec",
	}))
	packet := f.receiver.receive(t)
	assert.Equal(t, TrapSyncHoldover, trapOID(t, packet))
	assert.Contains(t, varbind(t, packet, "1.3.6.1.4.1.99999.2.1.1.0").Name, "1.3.6.1.4.1.99999.2.1.1.0")

	require.NoError(t, f.sender.Send(t.Context(), event.Event{
		Type:     event.TypeStateTransition,
		Resource: "ptp/clock",
		Domain:   event.DomainSync,
		From:     "holdover-in-spec",
		To:       "locked",
	}))
	assert.Equal(t, TrapSyncRestored, trapOID(t, f.receiver.receive(t)))
}

func TestSendStormAndConfigTraps(t *testing.T) {
	f := newTrapFixture(t, nil)

	require.NoError(t, f.sender.Send(t.Context(), event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "l2/storm/eth0",
		Severity: "major",
		Domain:   event.DomainL2,
		Alarm:    event.AlarmL2Storm,
	}))
	assert.Equal(t, TrapL2Storm, trapOID(t, f.receiver.receive(t)))

	require.NoError(t, f.sender.Send(t.Context(), event.Event{
		Type:     event.TypeConfigChanged,
		Resource: "device",
	}))
	assert.Equal(t, TrapConfigChanged, trapOID(t, f.receiver.receive(t)))
}

func TestEventWithoutTrapIsSilent(t *testing.T) {
	f := newTrapFixture(t, nil)

	require.NoError(t, f.sender.Send(t.Context(), event.Event{
		Type:     event.TypeStateTransition,
		Resource: "l2/stp/eth0",
		Domain:   event.DomainL2,
		From:     "discarding",
		To:       "forwarding",
	}))
	require.NoError(t, f.sender.Send(t.Context(), event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "ptp/clock",
		Severity: "major",
		Domain:   event.DomainSync,
		Alarm:    event.AlarmSyncHoldover,
	}))
	f.receiver.silent(t)
}

func TestSendRequiresListen(t *testing.T) {
	sender := NewTrapSender(TrapOptions{Addr: "127.0.0.1:1162"})

	err := sender.Send(t.Context(), event.Event{
		Type:  event.TypeConfigChanged,
		Alarm: event.AlarmL2Storm,
	})

	assert.ErrorContains(t, err, "not listening")
}

func TestListenRejectsABadDestination(t *testing.T) {
	sender := NewTrapSender(TrapOptions{Addr: "not-a-host:port"})

	assert.ErrorContains(t, sender.Listen(), "resolve trap destination")
}

func TestTrapForMapping(t *testing.T) {
	tests := []struct {
		name  string
		event event.Event
		want  string
	}{
		{name: "radio down", event: event.Event{Type: event.TypeAlarmRaised, Alarm: event.AlarmRadioLinkDown}, want: TrapRadioLinkDown},
		{name: "radio up", event: event.Event{Type: event.TypeAlarmCleared, Alarm: event.AlarmRadioLinkDown}, want: TrapRadioLinkUp},
		{name: "storm", event: event.Event{Type: event.TypeAlarmRaised, Alarm: event.AlarmL2Storm}, want: TrapL2Storm},
		{name: "holdover", event: event.Event{Type: event.TypeStateTransition, Domain: event.DomainSync, To: "holdover-in-spec"}, want: TrapSyncHoldover},
		{name: "holdover out of spec", event: event.Event{Type: event.TypeStateTransition, Domain: event.DomainSync, From: "holdover-in-spec", To: "holdover-out-of-spec"}, want: TrapSyncHoldover},
		{name: "restored", event: event.Event{Type: event.TypeStateTransition, Domain: event.DomainSync, From: "holdover-out-of-spec", To: "locked"}, want: TrapSyncRestored},
		{name: "config", event: event.Event{Type: event.TypeConfigChanged}, want: TrapConfigChanged},
		{name: "l2 transition", event: event.Event{Type: event.TypeStateTransition, Domain: event.DomainL2, To: "forwarding"}},
		{name: "sync alarm is not a transition trap", event: event.Event{Type: event.TypeAlarmRaised, Domain: event.DomainSync, Alarm: event.AlarmSyncHoldover}},
		{name: "degraded", event: event.Event{Type: event.TypeAlarmRaised, Alarm: event.AlarmRadioLinkDegraded}},
		{name: "unknown", event: event.Event{Type: "SomethingElse"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, trapFor(test.event))
		})
	}
}

// Run forwards the events of its bus subscription to the receiver.
func TestRunForwardsBusEvents(t *testing.T) {
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	t.Cleanup(bus.Close)

	f := newTrapFixture(t, bus)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.sender.Run(ctx)

	// New takes the subscription, so the event is queued even before Run drains
	// it.
	bus.Publish(event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "radio0",
		Severity: "critical",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	})

	assert.Equal(t, TrapRadioLinkDown, trapOID(t, f.receiver.receive(t)))
}

// A sender without a bus has nothing to run.
func TestRunWithoutBusReturns(t *testing.T) {
	sender := NewTrapSender(TrapOptions{Addr: "127.0.0.1:1162"})

	sender.Run(context.Background())
}
