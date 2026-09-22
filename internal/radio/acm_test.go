package radio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/model"
)

// seedProfiles returns the modulation-profile table of the seeded device.
func seedProfiles() []model.ModProfile {
	return model.DefaultDevice().Interfaces[0].RadioLink.Profiles
}

func TestSelectProfileAdaptive(t *testing.T) {
	profiles := seedProfiles()

	// Strong signal: the highest profile the window allows.
	profile, ok := selectProfile(model.ACM{Enabled: true, Mode: model.ACMModeAdaptive, MinProfile: 1, MaxProfile: 12}, profiles, -46.5)
	require.True(t, ok)
	assert.Equal(t, uint8(12), profile.ID)
	assert.Equal(t, uint32(308), profile.Capacity)

	// A weaker signal degrades to the profile whose threshold it still meets.
	profile, ok = selectProfile(model.ACM{Enabled: true, Mode: model.ACMModeAdaptive, MinProfile: 1, MaxProfile: 12}, profiles, -78.5)
	require.True(t, ok)
	assert.Equal(t, uint8(4), profile.ID)

	// The window caps the selection: profile 5 even though 12 would fit.
	profile, ok = selectProfile(model.ACM{Enabled: true, Mode: model.ACMModeAdaptive, MinProfile: 1, MaxProfile: 5}, profiles, -46.5)
	require.True(t, ok)
	assert.Equal(t, uint8(5), profile.ID)

	// A level below every threshold degrades to the most robust profile in the
	// window.
	profile, ok = selectProfile(model.ACM{Enabled: true, Mode: model.ACMModeAdaptive, MinProfile: 1, MaxProfile: 5}, profiles, -95)
	require.True(t, ok)
	assert.Equal(t, uint8(1), profile.ID)
}

func TestSelectProfileFixedAndDisabled(t *testing.T) {
	profiles := seedProfiles()

	// A fixed ACM keeps the configured current profile regardless of the level.
	profile, ok := selectProfile(model.ACM{Enabled: true, Mode: model.ACMModeFixed, MinProfile: 1, MaxProfile: 12, CurrentProfile: 5}, profiles, -46.5)
	require.True(t, ok)
	assert.Equal(t, uint8(5), profile.ID)

	// A disabled ACM does the same.
	profile, ok = selectProfile(model.ACM{Enabled: false, Mode: model.ACMModeAdaptive, MinProfile: 1, MaxProfile: 12, CurrentProfile: 7}, profiles, -46.5)
	require.True(t, ok)
	assert.Equal(t, uint8(7), profile.ID)

	// A current profile the table does not hold falls back to min-profile.
	profile, ok = selectProfile(model.ACM{Enabled: true, Mode: model.ACMModeFixed, MinProfile: 3, MaxProfile: 12, CurrentProfile: 0}, profiles, -46.5)
	require.True(t, ok)
	assert.Equal(t, uint8(3), profile.ID)
}

func TestSelectProfileWithoutProfiles(t *testing.T) {
	_, ok := selectProfile(model.ACM{Enabled: true, Mode: model.ACMModeAdaptive, MinProfile: 1, MaxProfile: 12}, nil, -46.5)
	assert.False(t, ok)

	// A window that holds no profile of the table has no fallback either.
	_, ok = selectProfile(model.ACM{Enabled: true, Mode: model.ACMModeAdaptive, MinProfile: 1, MaxProfile: 2}, seedProfiles()[3:], -46.5)
	assert.False(t, ok)
}
