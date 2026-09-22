package l2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// newTestManager returns a manager over a router seeded with DefaultDevice and
// the store behind it.
func newTestManager(t *testing.T) (*Manager, *router.Router, *store.Memory) {
	t.Helper()

	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	require.NoError(t, r.Seed(t.Context(), store.Running))
	require.NoError(t, st.Rollback(t.Context()))

	manager, err := New(Deps{Router: r, Clock: clock.NewFakeClock()})
	require.NoError(t, err)
	return manager, r, st
}

func TestNewRequiresRouter(t *testing.T) {
	manager, err := New(Deps{})
	assert.Nil(t, manager)
	assert.ErrorContains(t, err, "router must not be nil")
}

func TestVLANsReturnsSeed(t *testing.T) {
	manager, _, _ := newTestManager(t)

	vlans, err := manager.VLANs(t.Context())
	require.NoError(t, err)
	require.Len(t, vlans, 1)
	assert.Equal(t, uint16(100), vlans[0].ID)
	assert.Equal(t, "DATA", vlans[0].Name)
	require.Len(t, vlans[0].Ports, 1)
	assert.Equal(t, "eth0", vlans[0].Ports[0].Port)
}

func TestVLANUnknown(t *testing.T) {
	manager, _, _ := newTestManager(t)

	_, err := manager.VLAN(t.Context(), 999)
	assert.ErrorIs(t, err, ErrVLANNotFound)

	_, err = manager.Members(t.Context(), 999)
	assert.ErrorIs(t, err, ErrVLANNotFound)
}

func TestCreateVLAN(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	err := manager.CreateVLAN(ctx, model.VLAN{
		ID:   200,
		Name: "VOICE",
		Ports: []model.VLANPort{
			{Port: "eth2", Mode: model.VLANPortModeTrunk, PVID: 200, Tagged: true},
			{Port: "eth1", Mode: model.VLANPortModeAccess, PVID: 200},
		},
	})
	require.NoError(t, err)

	vlan, err := manager.VLAN(ctx, 200)
	require.NoError(t, err)
	assert.Equal(t, "VOICE", vlan.Name)

	members, err := manager.Members(ctx, 200)
	require.NoError(t, err)
	require.Len(t, members, 2)
	// Members are ordered by port name, not by insertion order.
	assert.Equal(t, []string{"eth1", "eth2"}, []string{members[0].Port, members[1].Port})
	assert.True(t, members[1].Tagged)
	assert.Equal(t, model.VLANPortModeAccess, members[0].Mode)

	// Every written leaf is addressable through the router.
	result, err := r.Get(ctx, store.Running, "vlans/vlan[id=200]/name")
	require.NoError(t, err)
	assert.Equal(t, "VOICE", result.Value)
	result, err = r.Get(ctx, store.Running, "vlans/vlan[id=200]/ports/port[port=eth2]/pvid")
	require.NoError(t, err)
	assert.Equal(t, uint16(200), result.Value)
}

func TestCreateVLANDuplicate(t *testing.T) {
	manager, _, _ := newTestManager(t)

	err := manager.CreateVLAN(t.Context(), model.VLAN{ID: 100, Name: "OTHER"})
	assert.ErrorIs(t, err, ErrVLANExists)
}

func TestCreateVLANValidation(t *testing.T) {
	tests := []struct {
		name    string
		vlan    model.VLAN
		wantErr string
	}{
		{
			name:    "id zero",
			vlan:    model.VLAN{ID: 0, Name: "NULL"},
			wantErr: "out of range",
		},
		{
			name:    "id above 4094",
			vlan:    model.VLAN{ID: 4095, Name: "RESERVED"},
			wantErr: "out of range",
		},
		{
			name:    "empty name",
			vlan:    model.VLAN{ID: 10},
			wantErr: "name must not be empty",
		},
		{
			name: "unknown port mode",
			vlan: model.VLAN{ID: 10, Name: "TEN", Ports: []model.VLANPort{
				{Port: "eth1", Mode: "promiscuous", PVID: 10},
			}},
			wantErr: "unknown mode",
		},
		{
			name: "access port tagged",
			vlan: model.VLAN{ID: 10, Name: "TEN", Ports: []model.VLANPort{
				{Port: "eth1", Mode: model.VLANPortModeAccess, PVID: 10, Tagged: true},
			}},
			wantErr: "access mode must not be tagged",
		},
		{
			name: "qinq outer equals inner",
			vlan: model.VLAN{ID: 10, Name: "TEN", Ports: []model.VLANPort{
				{Port: "eth1", Mode: model.VLANPortModeTrunk, PVID: 10, Tagged: true,
					QinQ: true, OuterVID: 10, CTagHandling: model.CTagHandlingPush},
			}},
			wantErr: "must differ from the inner pvid",
		},
		{
			name: "qinq outer out of range",
			vlan: model.VLAN{ID: 10, Name: "TEN", Ports: []model.VLANPort{
				{Port: "eth1", Mode: model.VLANPortModeTrunk, PVID: 10, Tagged: true,
					QinQ: true, OuterVID: 0, CTagHandling: model.CTagHandlingPush},
			}},
			wantErr: "s-tag-vid 0 out of range",
		},
		{
			name: "qinq without c-tag-handling",
			vlan: model.VLAN{ID: 10, Name: "TEN", Ports: []model.VLANPort{
				{Port: "eth1", Mode: model.VLANPortModeTrunk, PVID: 10, Tagged: true,
					QinQ: true, OuterVID: 20},
			}},
			wantErr: "unknown c-tag-handling",
		},
		{
			name: "outer tag without qinq",
			vlan: model.VLAN{ID: 10, Name: "TEN", Ports: []model.VLANPort{
				{Port: "eth1", Mode: model.VLANPortModeTrunk, PVID: 10, Tagged: true, OuterVID: 20},
			}},
			wantErr: "requires qinq",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, r, _ := newTestManager(t)

			err := manager.CreateVLAN(t.Context(), test.vlan)
			require.ErrorContains(t, err, test.wantErr)

			// A rejected VLAN leaves nothing behind.
			leaves, err := r.List(t.Context(), store.Running, vlanPrefix(test.vlan.ID))
			require.NoError(t, err)
			assert.Empty(t, leaves)
		})
	}
}

func TestCreateVLANWithQinQ(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.CreateVLAN(ctx, model.VLAN{
		ID:   300,
		Name: "SERVICE",
		Ports: []model.VLANPort{
			{Port: "eth3", Mode: model.VLANPortModeTrunk, PVID: 100, Tagged: true,
				QinQ: true, OuterVID: 300, CTagHandling: model.CTagHandlingPush},
		},
	}))

	member, err := manager.Member(ctx, 300, "eth3")
	require.NoError(t, err)
	assert.True(t, member.QinQ)
	assert.Equal(t, uint16(300), member.OuterVID)
	assert.Equal(t, model.CTagHandlingPush, member.CTagHandling)

	inner, outer, err := manager.TagStack(ctx, 300, "eth3")
	require.NoError(t, err)
	assert.Equal(t, uint16(100), inner)
	assert.Equal(t, uint16(300), outer)
}

func TestReplaceVLAN(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.ReplaceVLAN(ctx, model.VLAN{
		ID:          100,
		Name:        "DATA-RENAMED",
		Description: "renamed by test",
		Ports: []model.VLANPort{
			{Port: "eth1", Mode: model.VLANPortModeAccess, PVID: 100},
		},
	}))

	vlan, err := manager.VLAN(ctx, 100)
	require.NoError(t, err)
	assert.Equal(t, "DATA-RENAMED", vlan.Name)
	assert.Equal(t, "renamed by test", vlan.Description)
	require.Len(t, vlan.Ports, 1)
	assert.Equal(t, "eth1", vlan.Ports[0].Port)

	_, err = manager.Member(ctx, 100, "eth0")
	assert.ErrorIs(t, err, ErrMemberNotFound)
}

func TestReplaceVLANRejectsBrokenMemberList(t *testing.T) {
	manager, _, _ := newTestManager(t)

	err := manager.ReplaceVLAN(t.Context(), model.VLAN{
		ID:   100,
		Name: "DATA",
		Ports: []model.VLANPort{
			{Port: "eth1", Mode: "invalid", PVID: 100},
		},
	})
	assert.ErrorContains(t, err, "unknown mode")

	// Validation runs before any write, so the stored members are untouched.
	members, err := manager.Members(t.Context(), 100)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "eth0", members[0].Port)
}

func TestDeleteVLAN(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.DeleteVLAN(ctx, 100))

	_, err := manager.VLAN(ctx, 100)
	assert.ErrorIs(t, err, ErrVLANNotFound)

	leaves, err := r.List(ctx, store.Running, "vlans")
	require.NoError(t, err)
	assert.Empty(t, leaves)
}

func TestDeleteVLANUnknown(t *testing.T) {
	manager, _, _ := newTestManager(t)

	assert.ErrorIs(t, manager.DeleteVLAN(t.Context(), 42), ErrVLANNotFound)
}

func TestSetAndRemoveMember(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	// Update the seeded member.
	require.NoError(t, manager.SetMember(ctx, 100, model.VLANPort{
		Port: "eth0", Mode: model.VLANPortModeTrunk, PVID: 100, Tagged: true,
	}))
	member, err := manager.Member(ctx, 100, "eth0")
	require.NoError(t, err)
	assert.Equal(t, model.VLANPortModeTrunk, member.Mode)
	assert.True(t, member.Tagged)

	// Add a second member.
	require.NoError(t, manager.SetMember(ctx, 100, model.VLANPort{
		Port: "eth2", Mode: model.VLANPortModeAccess, PVID: 100,
	}))

	require.NoError(t, manager.RemoveMember(ctx, 100, "eth2"))
	_, err = manager.Member(ctx, 100, "eth2")
	assert.ErrorIs(t, err, ErrMemberNotFound)

	assert.ErrorIs(t, manager.RemoveMember(ctx, 100, "eth2"), ErrMemberNotFound)
	assert.ErrorIs(t, manager.RemoveMember(ctx, 999, "eth0"), ErrVLANNotFound)
}

func TestSetMemberValidation(t *testing.T) {
	manager, _, _ := newTestManager(t)

	err := manager.SetMember(t.Context(), 100, model.VLANPort{
		Port: "eth1", Mode: model.VLANPortModeAccess, PVID: 100, Tagged: true,
	})
	assert.ErrorContains(t, err, "access mode must not be tagged")

	err = manager.SetMember(t.Context(), 100, model.VLANPort{
		Port: "eth1", Mode: model.VLANPortModeTrunk, PVID: 100, Tagged: true,
		QinQ: true, OuterVID: 100, CTagHandling: model.CTagHandlingPop,
	})
	assert.ErrorContains(t, err, "must differ from the inner pvid")

	assert.ErrorIs(t, manager.SetMember(t.Context(), 999, model.VLANPort{
		Port: "eth1", Mode: model.VLANPortModeAccess, PVID: 100,
	}), ErrVLANNotFound)
}

func TestTagStackUntaggedPort(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.CreateVLAN(ctx, model.VLAN{
		ID:   400,
		Name: "UNTAGGED",
		Ports: []model.VLANPort{
			{Port: "eth1", Mode: model.VLANPortModeAccess, PVID: 400},
		},
	}))

	inner, outer, err := manager.TagStack(ctx, 400, "eth1")
	require.NoError(t, err)
	assert.Zero(t, inner)
	assert.Zero(t, outer)

	_, _, err = manager.TagStack(ctx, 400, "eth9")
	assert.ErrorIs(t, err, ErrMemberNotFound)
}

func TestPortVLANsAndAccepts(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.CreateVLAN(ctx, model.VLAN{
		ID:   200,
		Name: "VOICE",
		Ports: []model.VLANPort{
			{Port: "eth0", Mode: model.VLANPortModeTrunk, PVID: 200, Tagged: true},
		},
	}))

	vlans, err := manager.PortVLANs(ctx, "eth0")
	require.NoError(t, err)
	require.Len(t, vlans, 2)
	assert.Equal(t, uint16(100), vlans[0].ID)
	assert.Equal(t, uint16(200), vlans[1].ID)

	accepted, err := manager.Accepts(ctx, "eth0", 100)
	require.NoError(t, err)
	assert.True(t, accepted)

	accepted, err = manager.Accepts(ctx, "eth3", 100)
	require.NoError(t, err)
	assert.False(t, accepted)

	// An unknown VLAN is simply not accepted.
	accepted, err = manager.Accepts(ctx, "eth0", 999)
	require.NoError(t, err)
	assert.False(t, accepted)

	vlans, err = manager.PortVLANs(ctx, "eth3")
	require.NoError(t, err)
	assert.Empty(t, vlans)
}
