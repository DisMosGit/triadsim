package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// selectSyncESource is the selection order: quality level, priority, PTP
// preference, then name.
func TestSelectSyncESource(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		interfaces []model.SyncEInterface
		wantName   string
		wantQL     model.QL
	}{
		{
			name:    "best quality wins",
			enabled: true,
			interfaces: []model.SyncEInterface{
				{Name: "eth1", SSMEnabled: true, QL: model.QLSSUA, Priority: 1},
				{Name: "eth0", SSMEnabled: true, QL: model.QLPRC, Priority: 100},
			},
			wantName: "eth0",
			wantQL:   model.QLPRC,
		},
		{
			name:    "lower priority breaks a quality tie",
			enabled: true,
			interfaces: []model.SyncEInterface{
				{Name: "eth0", SSMEnabled: true, QL: model.QLPRC, Priority: 20},
				{Name: "eth1", SSMEnabled: true, QL: model.QLPRC, Priority: 5},
			},
			wantName: "eth1",
			wantQL:   model.QLPRC,
		},
		{
			name:    "ptp preference breaks a priority tie",
			enabled: true,
			interfaces: []model.SyncEInterface{
				{Name: "eth0", SSMEnabled: true, QL: model.QLPRC, Priority: 10},
				{Name: "eth1", SSMEnabled: true, QL: model.QLPRC, Priority: 10, PTPPreference: true},
			},
			wantName: "eth1",
			wantQL:   model.QLPRC,
		},
		{
			name:    "name breaks a full tie",
			enabled: true,
			interfaces: []model.SyncEInterface{
				{Name: "eth2", SSMEnabled: true, QL: model.QLPRC, Priority: 10},
				{Name: "eth1", SSMEnabled: true, QL: model.QLPRC, Priority: 10},
			},
			wantName: "eth1",
			wantQL:   model.QLPRC,
		},
		{
			name:    "dnu is never selected",
			enabled: true,
			interfaces: []model.SyncEInterface{
				{Name: "eth0", SSMEnabled: true, QL: model.QLDNU, Priority: 1},
				{Name: "eth1", SSMEnabled: true, QL: model.QLSEC, Priority: 20},
			},
			wantName: "eth1",
			wantQL:   model.QLSEC,
		},
		{
			name:    "ssm disabled is skipped",
			enabled: true,
			interfaces: []model.SyncEInterface{
				{Name: "eth0", SSMEnabled: false, QL: model.QLPRC, Priority: 1},
				{Name: "eth1", SSMEnabled: true, QL: model.QLSSUB, Priority: 20},
			},
			wantName: "eth1",
			wantQL:   model.QLSSUB,
		},
		{
			name:    "global switch off clears the selection",
			enabled: false,
			interfaces: []model.SyncEInterface{
				{Name: "eth0", SSMEnabled: true, QL: model.QLPRC, Priority: 1},
			},
		},
		{
			name:    "no candidate",
			enabled: true,
			interfaces: []model.SyncEInterface{
				{Name: "eth0", SSMEnabled: true, QL: model.QLDNU, Priority: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := model.DefaultDevice()
			device.SyncE.Enabled = tt.enabled
			device.SyncE.Interfaces = tt.interfaces

			name, ql, extended := selectSyncESource(device)

			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantQL, ql)
			assert.Empty(t, extended)
		})
	}
}

// The refresh writes the selection to the datastore and follows configuration
// changes within one tick.
func TestRefreshSyncE(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	require.NoError(t, f.manager.Tick(ctx))
	assert.Equal(t, "eth0", f.leaf(t, "synce/selected-source"))
	assert.Equal(t, string(model.QLPRC), f.leaf(t, "synce/selected-ql"))

	// eth1 becomes the better source: same quality, lower priority.
	_, err := f.router.Set(ctx, store.Running, "synce/interfaces/interface[name=eth1]/ql", string(model.QLPRC))
	require.NoError(t, err)
	_, err = f.router.Set(ctx, store.Running, "synce/interfaces/interface[name=eth1]/priority", 5)
	require.NoError(t, err)

	require.NoError(t, f.manager.Tick(ctx))
	assert.Equal(t, "eth1", f.leaf(t, "synce/selected-source"))
	assert.Equal(t, string(model.QLPRC), f.leaf(t, "synce/selected-ql"))

	// Disabling the global switch clears the selection.
	_, err = f.router.Set(ctx, store.Running, "synce/enabled", false)
	require.NoError(t, err)

	require.NoError(t, f.manager.Tick(ctx))
	assert.Equal(t, "", f.leaf(t, "synce/selected-source"))
	assert.Equal(t, "", f.leaf(t, "synce/selected-ql"))
	assert.Equal(t, "", f.leaf(t, "synce/selected-extended-ql"))
}

// A quality-level change that makes the selected source unselectable moves the
// selection to the remaining candidate.
func TestRefreshSyncEDropsDNU(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	require.NoError(t, f.manager.Tick(ctx))
	require.Equal(t, "eth0", f.leaf(t, "synce/selected-source"))

	_, err := f.router.Set(ctx, store.Running, "synce/interfaces/interface[name=eth0]/ql", string(model.QLDNU))
	require.NoError(t, err)

	require.NoError(t, f.manager.Tick(ctx))
	assert.Equal(t, "eth1", f.leaf(t, "synce/selected-source"))
	assert.Equal(t, string(model.QLSSUA), f.leaf(t, "synce/selected-ql"))
}
