package snmp

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

const (
	sysDescrOID  = "1.3.6.1.2.1.1.1.0"
	sysNameOID   = "1.3.6.1.2.1.1.5.0"
	ifDescrOID   = "1.3.6.1.2.1.2.2.1.2"
	rssiOID      = "1.3.6.1.4.1.99999.1.1.1.0"
	txPowerOID   = "1.3.6.1.4.1.99999.1.1.4.0"
	atpcOID      = "1.3.6.1.4.1.99999.1.1.5.0"
	ptpStateOID  = "1.3.6.1.4.1.99999.2.1.1.0"
	ptpDomainOID = "1.3.6.1.4.1.99999.2.1.3.0"
	syncEQLOID   = "1.3.6.1.4.1.99999.2.1.5.0"
)

// newTestRouter returns a router over a seeded Memory store.
func newTestRouter(t *testing.T) *router.Router {
	t.Helper()
	ctx := context.Background()

	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	require.NoError(t, r.Seed(ctx, store.Running))
	require.NoError(t, st.Rollback(ctx))
	return r
}

// startAgent serves an agent on an ephemeral loopback port until the test
// ends.
func startAgent(t *testing.T) *Agent {
	t.Helper()

	agent := New(newTestRouter(t), Options{Addr: "127.0.0.1:0"})
	require.NoError(t, agent.Listen())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Serve(ctx) }()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, agent.Close())
		require.NoError(t, <-done)
	})
	return agent
}

// newClient connects a real gosnmp client to addr.
func newClient(t *testing.T, addr net.Addr, community string) *gosnmp.GoSNMP {
	t.Helper()

	host, portText, err := net.SplitHostPort(addr.String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	client := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(port),
		Transport: "udp",
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   time.Second,
		Retries:   0,
	}
	require.NoError(t, client.Connect())
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

func TestGetSysDescr(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Get([]string{sysDescrOID})

	require.NoError(t, err)
	require.Equal(t, gosnmp.NoError, result.Error)
	require.Len(t, result.Variables, 1)
	assert.Equal(t, []byte("TriadSim simulated telecom device"), result.Variables[0].Value)
}

func TestWalkIfDescr(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	pdus, err := client.WalkAll(ifDescrOID)

	require.NoError(t, err)
	require.Len(t, pdus, 3)
	assert.Equal(t, []byte("radio0"), pdus[0].Value)
	assert.Equal(t, []byte("eth0"), pdus[1].Value)
	assert.Equal(t, []byte("eth1"), pdus[2].Value)
}

func TestBulkWalkIfDescr(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	pdus, err := client.BulkWalkAll(ifDescrOID)

	require.NoError(t, err)
	require.Len(t, pdus, 3)
	assert.Equal(t, []byte("radio0"), pdus[0].Value)
	assert.Equal(t, []byte("eth1"), pdus[2].Value)
}

func TestGetVendorRSSI(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Get([]string{rssiOID})

	require.NoError(t, err)
	require.Len(t, result.Variables, 1)
	assert.Equal(t, gosnmp.OpaqueDouble, result.Variables[0].Type)
	assert.Equal(t, -72.5, result.Variables[0].Value)
}

// The vendor synchronization objects answer the seeded state: a locked clock on
// domain 24 whose selected SyncE source carries QL-PRC(2).
func TestGetVendorSync(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Get([]string{ptpStateOID, ptpDomainOID, syncEQLOID})

	require.NoError(t, err)
	require.Len(t, result.Variables, 3)
	assert.Equal(t, 3, result.Variables[0].Value, "the seeded PTP clock is locked")
	assert.EqualValues(t, 24, result.Variables[1].Value)
	assert.Equal(t, 2, result.Variables[2].Value, "eth0 carries QL-PRC(2)")
}

// A writable sync object goes through the same whole-device validation as the
// radio objects.
func TestSetPTPDomain(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Set([]gosnmp.SnmpPDU{{Name: ptpDomainOID, Type: gosnmp.Gauge32, Value: uint32(25)}})
	require.NoError(t, err)
	require.Equal(t, gosnmp.NoError, result.Error)

	got, err := client.Get([]string{ptpDomainOID})
	require.NoError(t, err)
	assert.EqualValues(t, 25, got.Variables[0].Value)

	// A domain outside 0..127 is rejected by the model.
	result, err = client.Set([]gosnmp.SnmpPDU{{Name: ptpDomainOID, Type: gosnmp.Gauge32, Value: uint32(200)}})
	require.NoError(t, err)
	assert.Equal(t, gosnmp.WrongValue, result.Error)
}

func TestGetUnknownOID(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Get([]string{"1.3.6.1.2.1.99.1.0"})

	require.NoError(t, err)
	assert.Equal(t, gosnmp.NoError, result.Error)
	require.Len(t, result.Variables, 1)
	assert.Equal(t, gosnmp.NoSuchObject, result.Variables[0].Type)
}

func TestGetNextPastEnd(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.GetNext([]string{"1.3.6.1.4.1.99999.9"})

	require.NoError(t, err)
	require.Len(t, result.Variables, 1)
	assert.Equal(t, gosnmp.EndOfMibView, result.Variables[0].Type)
}

func TestSetTxPower(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Set([]gosnmp.SnmpPDU{{Name: txPowerOID, Type: gosnmp.OpaqueDouble, Value: 25.5}})

	require.NoError(t, err)
	require.Equal(t, gosnmp.NoError, result.Error)
	assert.Equal(t, 25.5, result.Variables[0].Value)

	got, err := client.Get([]string{txPowerOID})
	require.NoError(t, err)
	assert.Equal(t, 25.5, got.Variables[0].Value)
}

func TestSetSysNameAndATPC(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Set([]gosnmp.SnmpPDU{
		{Name: sysNameOID, Type: gosnmp.OctetString, Value: "triadsim-02"},
		{Name: atpcOID, Type: gosnmp.Integer, Value: 2},
	})
	require.NoError(t, err)
	require.Equal(t, gosnmp.NoError, result.Error)

	got, err := client.Get([]string{sysNameOID, atpcOID})
	require.NoError(t, err)
	assert.Equal(t, []byte("triadsim-02"), got.Variables[0].Value)
	assert.Equal(t, 2, got.Variables[1].Value)
}

func TestSetReadOnlyIsRejected(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Set([]gosnmp.SnmpPDU{{Name: rssiOID, Type: gosnmp.OpaqueDouble, Value: -50.0}})

	require.NoError(t, err)
	assert.Equal(t, gosnmp.ReadOnly, result.Error)

	// The value is unchanged.
	got, err := client.Get([]string{rssiOID})
	require.NoError(t, err)
	assert.Equal(t, -72.5, got.Variables[0].Value)
}

func TestSetOutOfRangeIsRejected(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Set([]gosnmp.SnmpPDU{{Name: txPowerOID, Type: gosnmp.OpaqueDouble, Value: 999.0}})

	require.NoError(t, err)
	assert.Equal(t, gosnmp.WrongValue, result.Error)

	got, err := client.Get([]string{txPowerOID})
	require.NoError(t, err)
	assert.Equal(t, 20.0, got.Variables[0].Value)
}

func TestWrongCommunityIsDropped(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), "private")

	_, err := client.Get([]string{sysDescrOID})

	assert.Error(t, err, "a request with the wrong community must not be answered")
}

func TestServeReturnsOnCancel(t *testing.T) {
	agent := New(newTestRouter(t), Options{Addr: "127.0.0.1:0"})
	require.NoError(t, agent.Listen())
	defer func() { require.NoError(t, agent.Close()) }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Serve(ctx) }()

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after cancellation")
	}
}

func TestServeRequiresListen(t *testing.T) {
	agent := New(newTestRouter(t), Options{Addr: "127.0.0.1:0"})

	assert.Error(t, agent.Serve(context.Background()))
}

// fdbPortOID is dot1dTpFdbPort, the bridge forwarding database port column.
const fdbPortOID = "1.3.6.1.2.1.17.4.3.1.2"

// A walk of dot1dTpFdbPort reports every forwarding-database entry as
// MAC -> bridge port, which is the Phase 4 acceptance criterion.
func TestWalkBridgeFdbPort(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	pdus, err := client.WalkAll(fdbPortOID)
	require.NoError(t, err)
	require.Len(t, pdus, 1, "the seeded device has one static forwarding entry")

	// 02:00:00:00:00:02 is the eth1 MAC and 3 is its bridge port number.
	assert.Equal(t, fdbPortOID+".2.0.0.0.0.2", strings.TrimPrefix(pdus[0].Name, "."))
	assert.Equal(t, gosnmp.Integer, pdus[0].Type)
	assert.Equal(t, 3, pdus[0].Value)
}

func TestGetBridgeIdentityAndSTPPort(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Get([]string{
		"1.3.6.1.2.1.17.1.1.0",
		"1.3.6.1.2.1.17.2.15.1.3.2",
		"1.3.6.1.2.1.17.7.1.4.3.1.1.100",
	})
	require.NoError(t, err)
	require.Equal(t, gosnmp.NoError, result.Error)

	assert.Equal(t, []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}, result.Variables[0].Value)
	assert.Equal(t, 5, result.Variables[1].Value, "eth0 forwards")
	assert.Equal(t, []byte("DATA"), result.Variables[2].Value)
}

func TestGetHighCapacityCounter(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Get([]string{"1.3.6.1.2.1.31.1.1.1.6.2", "1.3.6.1.2.1.2.2.1.10.2"})
	require.NoError(t, err)
	require.Equal(t, gosnmp.NoError, result.Error)

	assert.Equal(t, gosnmp.Counter64, result.Variables[0].Type)
	assert.Equal(t, uint64(0), result.Variables[0].Value)
	assert.Equal(t, gosnmp.Counter32, result.Variables[1].Type)
}

func TestSetBridgeAgingTime(t *testing.T) {
	agent := startAgent(t)
	client := newClient(t, agent.Addr(), DefaultCommunity)

	result, err := client.Set([]gosnmp.SnmpPDU{
		{Name: "1.3.6.1.2.1.17.4.2.0", Type: gosnmp.Integer, Value: 600},
	})
	require.NoError(t, err)
	require.Equal(t, gosnmp.NoError, result.Error)

	got, err := client.Get([]string{"1.3.6.1.2.1.17.4.2.0"})
	require.NoError(t, err)
	assert.Equal(t, 600, got.Variables[0].Value)

	// The model bounds the aging time, so a short one is rejected.
	result, err = client.Set([]gosnmp.SnmpPDU{
		{Name: "1.3.6.1.2.1.17.4.2.0", Type: gosnmp.Integer, Value: 1},
	})
	require.NoError(t, err)
	assert.Equal(t, gosnmp.WrongValue, result.Error)
}
