package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfig writes content to a temporary file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    func(*Config)
	}{
		{
			name:    "empty file keeps all defaults",
			content: "\n# comments only\n",
			want:    func(*Config) {},
		},
		{
			name:    "partial file overrides only present fields",
			content: "log:\n  level: debug\n",
			want:    func(c *Config) { c.Log.Level = LevelDebug },
		},
		{
			name:    "gnmi section overrides enabled and port",
			content: "gnmi:\n  enabled: true\n  port: 9443\n",
			want: func(c *Config) {
				c.GNMI.Enabled = true
				c.GNMI.Port = 9443
			},
		},
		{
			name: "full file overrides everything",
			content: `
snmp:
  port: 2161
  trap-host: 10.0.0.9
  trap-port: 2162
netconf:
  port: 1830
restconf:
  port: 18080
metrics:
  port: 19090
gnmi:
  enabled: true
  port: 19339
log:
  level: warn
startup:
  file: /tmp/triadsim-startup.json
`,
			want: func(c *Config) {
				c.SNMP = SNMP{Port: 2161, TrapHost: "10.0.0.9", TrapPort: 2162}
				c.NETCONF = NETCONF{Port: 1830}
				c.RESTCONF = RESTCONF{Port: 18080}
				c.Metrics = Metrics{Port: 19090}
				c.GNMI = GNMI{Enabled: true, Port: 19339}
				c.Log = Log{Level: LevelWarn}
				c.Startup = Startup{File: "/tmp/triadsim-startup.json"}
			},
		},
		{
			name:    "log level is case-insensitive",
			content: "log:\n  level: ERROR\n",
			want:    func(c *Config) { c.Log.Level = "ERROR" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := Default()
			tt.want(want)

			got, err := Load(writeConfig(t, tt.content))
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestLoadRejectsInvalidFiles(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "unknown key",
			content: "log:\n  levl: info\n",
			wantErr: "field levl not found",
		},
		{
			name:    "malformed yaml",
			content: "snmp: [unclosed\n",
			wantErr: "parse",
		},
		{
			name:    "wrong type for port",
			content: "snmp:\n  port: not-a-number\n",
			wantErr: "parse",
		},
		{
			name:    "port below range",
			content: "snmp:\n  port: 0\n",
			wantErr: "out of range",
		},
		{
			name:    "port above range",
			content: "restconf:\n  port: 70000\n",
			wantErr: "out of range",
		},
		{
			name:    "duplicate port",
			content: "snmp:\n  port: 8080\n",
			wantErr: "already used by",
		},
		{
			name:    "gnmi port collides with an enabled plane",
			content: "gnmi:\n  enabled: true\n  port: 1161\n",
			wantErr: "already used by",
		},
		{
			name:    "unknown log level",
			content: "log:\n  level: verbose\n",
			wantErr: "unknown level",
		},
		{
			name:    "empty trap host",
			content: "snmp:\n  trap-host: \" \"\n",
			wantErr: "snmp.trap-host",
		},
		{
			name:    "empty startup file",
			content: "startup:\n  file: \"\"\n",
			wantErr: "startup.file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(writeConfig(t, tt.content))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, got)
		})
	}
}

func TestLoadFileErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := Load("  ")
		require.Error(t, err)
		assert.ErrorContains(t, err, "path is empty")
	})
}

func TestLoadShippedDefaultFile(t *testing.T) {
	path := filepath.Join("..", "..", "configs", "default.yaml")

	got, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, Default(), got)
}

func TestDefaultIsValid(t *testing.T) {
	assert.NoError(t, Default().Validate())
}

func TestValidateIgnoresGNMIPortWhenDisabled(t *testing.T) {
	cfg := Default()
	cfg.GNMI.Port = cfg.SNMP.Port

	assert.NoError(t, cfg.Validate(), "disabled gNMI must not be checked")

	cfg.GNMI.Enabled = true
	assert.ErrorContains(t, cfg.Validate(), "already used by")
}

func TestValidateDoesNotModifyLogLevel(t *testing.T) {
	cfg := Default()
	cfg.Log.Level = "InFo"

	require.NoError(t, cfg.Validate())
	assert.Equal(t, "InFo", cfg.Log.Level)
}

func TestLoadWrapsValidationErrorWithPath(t *testing.T) {
	path := writeConfig(t, "snmp:\n  port: 0\n")

	_, err := Load(path)
	require.Error(t, err)
	assert.ErrorContains(t, err, path)
	assert.ErrorContains(t, err, "out of range")
}
