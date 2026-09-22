package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")
	values := map[string]any{
		"interfaces/interface[name=radio0]/radio-link/tx-power":        20.5,
		"interfaces/interface[name=radio0]/enabled":                    true,
		"interfaces/interface[name=radio0]/mtu":                        uint32(1500),
		"interfaces/interface[name=radio0]/radio-link/acm/min-profile": uint8(1),
		"vlans/vlan[id=100]/pvid":                                      uint16(100),
		"interfaces/interface[name=eth0]/counters/in-octets":           uint64(1) << 40,
		"sync/ptp/domain":       24,
		"system-info/device-id": "sim-001",
	}

	require.NoError(t, Save(ctx, path, values))

	got, err := Load(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, values, got)

	for p, want := range values {
		assert.IsType(t, want, got[p], "type of %s must survive the round trip", p)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "absent.json")

	got, err := Load(ctx, path)

	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestLoadMalformedJSON(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	_, err := Load(ctx, path)

	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrInvalidValue))
}

func TestLoadUnsupportedVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":99,"values":{}}`), 0o600))

	_, err := Load(ctx, path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported version")
}

func TestLoadUnknownKind(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")
	body := `{"version":1,"values":{"a/1":{"kind":"complex","value":1}}}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	_, err := Load(ctx, path)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidValue))
}

func TestLoadValueMismatchesKind(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")
	body := `{"version":1,"values":{"a/1":{"kind":"int","value":"x"}}}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	_, err := Load(ctx, path)

	require.Error(t, err)
}

func TestSaveRejectsUnsupportedValue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")

	err := Save(ctx, path, map[string]any{"a/1": int64(1)})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidValue))

	_, statErr := os.Stat(path)
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "a rejected value must not write the file")
}

func TestSaveEmptyMap(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")

	require.NoError(t, Save(ctx, path, nil))

	got, err := Load(ctx, path)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestSaveOverwrites(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")

	require.NoError(t, Save(ctx, path, map[string]any{"a/1": 1}))
	require.NoError(t, Save(ctx, path, map[string]any{"a/2": 2}))

	got, err := Load(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a/2": 2}, got)
}

func TestSaveLoadEmptyPath(t *testing.T) {
	ctx := context.Background()

	assert.Error(t, Save(ctx, "", map[string]any{"a/1": 1}))

	_, err := Load(ctx, "")
	assert.Error(t, err)
}

func TestSaveLoadCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "startup.json")

	err := Save(ctx, path, map[string]any{"a/1": 1})
	assert.True(t, errors.Is(err, context.Canceled))

	_, err = Load(ctx, path)
	assert.True(t, errors.Is(err, context.Canceled))
}
