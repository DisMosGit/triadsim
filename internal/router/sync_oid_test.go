package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/store"
)

// The vendor synchronization subtree of the seeded device.
func TestBindingsExposeSyncMIB(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	bindings, err := r.Bindings(ctx, store.Running)
	require.NoError(t, err)

	byOID := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		byOID[binding.OID] = binding
	}

	// Scalars: the clock is a master on domain 24, locked to its reference.
	state, ok := byOID[EnterpriseOID+".2.1.1.0"]
	require.True(t, ok, "simSyncPtpState")
	assert.Equal(t, TypeInteger, state.Type)
	assert.Equal(t, 3, state.Value, "the seeded clock is locked")
	assert.Equal(t, "ptp/clock/state", state.Path)
	assert.False(t, state.Writable)

	assert.Equal(t, 0.0, byOID[EnterpriseOID+".2.1.2.0"].Value)
	assert.Equal(t, uint8(24), byOID[EnterpriseOID+".2.1.3.0"].Value)
	assert.True(t, byOID[EnterpriseOID+".2.1.3.0"].Writable, "the PTP domain is writable")
	assert.Equal(t, uint8(128), byOID[EnterpriseOID+".2.1.4.0"].Value)
	assert.True(t, byOID[EnterpriseOID+".2.1.4.0"].Writable, "priority1 is writable")

	// The selected SyncE source is eth0 with QL-PRC(2) and no eSSM code.
	assert.Equal(t, 2, byOID[EnterpriseOID+".2.1.5.0"].Value)
	assert.Equal(t, 0, byOID[EnterpriseOID+".2.1.6.0"].Value)

	// The SyncE interface table is indexed by the interface index: radio0=1,
	// eth0=2, eth1=3.
	eth0, ok := byOID[EnterpriseOID+".2.1.7.1.1.2"]
	require.True(t, ok, "simSyncSyncEIfQL of eth0")
	assert.Equal(t, 2, eth0.Value, "eth0 carries QL-PRC(2)")
	assert.Equal(t, "synce/interfaces/interface[name=eth0]/ql", eth0.Path)

	eth1, ok := byOID[EnterpriseOID+".2.1.7.1.1.3"]
	require.True(t, ok, "simSyncSyncEIfQL of eth1")
	assert.Equal(t, 4, eth1.Value, "eth1 carries QL-SSU-A(4)")

	assert.Equal(t, 1, byOID[EnterpriseOID+".2.1.7.1.2.2"].Value, "eth0 has SSM enabled")
	assert.Equal(t, 0.0, byOID[EnterpriseOID+".2.1.8.0"].Value)

	// Bindings stay sorted by numeric OID with the new subtree.
	for i := 1; i < len(bindings); i++ {
		assert.Equal(t, -1, CompareOID(bindings[i-1].OID, bindings[i].OID))
	}
}

func TestSyncOIDRoundTrip(t *testing.T) {
	r, _ := newTestRouter(t)

	for path, want := range map[string]string{
		"ptp/clock/state":                          EnterpriseOID + ".2.1.1.0",
		"ptp/clock/offset":                         EnterpriseOID + ".2.1.2.0",
		"ptp/clock/domain":                         EnterpriseOID + ".2.1.3.0",
		"synce/selected-ql":                        EnterpriseOID + ".2.1.5.0",
		"ptp/clock/jitter":                         EnterpriseOID + ".2.1.8.0",
		"synce/interfaces/interface[name=eth0]/ql": EnterpriseOID + ".2.1.7.1.1.2",
	} {
		oid, ok := r.OIDForPath(path)
		require.True(t, ok, "OIDForPath(%s)", path)
		assert.Equal(t, want, oid)

		parsed, ok := r.PathForOID(oid)
		require.True(t, ok, "PathForOID(%s)", oid)
		assert.Equal(t, path, parsed.String())
	}
}

func TestSetPTPDomain(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	result, err := r.Set(ctx, store.Running, "ptp/clock/domain", 30)
	require.NoError(t, err)
	assert.Equal(t, uint8(30), result.Value)

	// The read-only clock state cannot be written by a management plane.
	_, err = r.Set(ctx, store.Running, "ptp/clock/state", "holdover-in-spec")
	assert.ErrorIs(t, err, ErrReadOnly)
}

func TestSyncSNMPConverters(t *testing.T) {
	tests := []struct {
		name    string
		convert func(any) any
		value   any
		want    any
	}{
		{name: "freerun", convert: ptpStateValue, value: "freerun", want: 1},
		{name: "acquiring", convert: ptpStateValue, value: "acquiring", want: 2},
		{name: "locked", convert: ptpStateValue, value: "locked", want: 3},
		{name: "holdover in spec", convert: ptpStateValue, value: "holdover-in-spec", want: 4},
		{name: "holdover out of spec", convert: ptpStateValue, value: "holdover-out-of-spec", want: 5},
		{name: "unknown state", convert: ptpStateValue, value: "synced", want: 1},
		{name: "non string state", convert: ptpStateValue, value: 3, want: 3},
		{name: "QL PRC", convert: qlValue, value: "QL-PRC", want: 2},
		{name: "QL SSU-A", convert: qlValue, value: "QL-SSU-A", want: 4},
		{name: "QL SSU-B", convert: qlValue, value: "QL-SSU-B", want: 8},
		{name: "QL SEC", convert: qlValue, value: "QL-SEC", want: 11},
		{name: "QL DNU", convert: qlValue, value: "QL-DNU", want: 15},
		{name: "QL empty", convert: qlValue, value: "", want: 0},
		{name: "QL unknown", convert: qlValue, value: "QL-NOPE", want: 0},
		{name: "eSSM PRTC", convert: extendedQLValue, value: "QL-PRTC", want: 0x20},
		{name: "eSSM ePRTC", convert: extendedQLValue, value: "QL-ePRTC", want: 0x21},
		{name: "eSSM eEEC", convert: extendedQLValue, value: "QL-eEEC", want: 0x22},
		{name: "eSSM empty", convert: extendedQLValue, value: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.convert(tt.value))
		})
	}
}
