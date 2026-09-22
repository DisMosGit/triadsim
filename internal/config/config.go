// Package config loads and validates the simulator's YAML configuration.
//
// Load starts from Default, so a partial file only needs to override the
// fields it cares about. Decoding is strict: unknown keys are an error, which
// turns a typo in configs/default.yaml into a startup failure instead of a
// silently ignored setting. Validate checks the result, so callers never have
// to re-derive the invariants.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Log levels accepted in Config.Log.Level.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// DefaultPath is the configuration file used when --config is not given.
const DefaultPath = "configs/default.yaml"

// Default ports. Every port is unprivileged so the simulator runs without root:
// SNMP uses 1161 instead of 161 and NETCONF uses 1830 instead of the IANA 830.
const (
	DefaultSNMPPort     = 1161
	DefaultSNMPTrapPort = 1162
	DefaultNETCONFPort  = 1830
	DefaultRESTCONFPort = 8080
	DefaultMetricsPort  = 9090
	DefaultGNMIPort     = 9339
)

// Config is the complete simulator configuration.
type Config struct {
	SNMP     SNMP     `yaml:"snmp"`
	NETCONF  NETCONF  `yaml:"netconf"`
	RESTCONF RESTCONF `yaml:"restconf"`
	Metrics  Metrics  `yaml:"metrics"`
	GNMI     GNMI     `yaml:"gnmi"`
	Log      Log      `yaml:"log"`
	Startup  Startup  `yaml:"startup"`
}

// SNMP configures the SNMP v2c agent and its trap sender.
type SNMP struct {
	Port     int `yaml:"port"`
	TrapPort int `yaml:"trap-port"`
}

// NETCONF configures the NETCONF SSH subsystem.
type NETCONF struct {
	Port int `yaml:"port"`
}

// RESTCONF configures the RESTCONF HTTP server.
type RESTCONF struct {
	Port int `yaml:"port"`
}

// Metrics configures the Prometheus /metrics endpoint.
type Metrics struct {
	Port int `yaml:"port"`
}

// GNMI configures the optional gNMI service.
type GNMI struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// Log configures structured logging.
type Log struct {
	Level string `yaml:"level"`
}

// Startup configures the persisted startup datastore.
type Startup struct {
	File string `yaml:"file"`
}

// Default returns the built-in configuration. It is always valid.
func Default() *Config {
	return &Config{
		SNMP:     SNMP{Port: DefaultSNMPPort, TrapPort: DefaultSNMPTrapPort},
		NETCONF:  NETCONF{Port: DefaultNETCONFPort},
		RESTCONF: RESTCONF{Port: DefaultRESTCONFPort},
		Metrics:  Metrics{Port: DefaultMetricsPort},
		GNMI:     GNMI{Enabled: false, Port: DefaultGNMIPort},
		Log:      Log{Level: LevelInfo},
		Startup:  Startup{File: "startup.json"},
	}
}

// Load reads the YAML file at path on top of Default, rejects unknown keys and
// validates the result. Reading path must succeed; an empty (or comments-only)
// file is valid and yields Default.
func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("config: path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

// Validate reports whether the configuration is usable. It never modifies the
// receiver. Ports of enabled planes must be in 1-65535 and pairwise distinct,
// Log.Level must be one of debug, info, warn or error (case-insensitive), and
// Startup.File must be set.
func (c *Config) Validate() error {
	type port struct {
		name string
		port int
	}
	ports := []port{
		{"snmp.port", c.SNMP.Port},
		{"snmp.trap-port", c.SNMP.TrapPort},
		{"netconf.port", c.NETCONF.Port},
		{"restconf.port", c.RESTCONF.Port},
		{"metrics.port", c.Metrics.Port},
	}
	if c.GNMI.Enabled {
		ports = append(ports, port{"gnmi.port", c.GNMI.Port})
	}

	seen := make(map[int]string, len(ports))
	for _, p := range ports {
		if p.port < 1 || p.port > 65535 {
			return fmt.Errorf("%s: port %d out of range 1-65535", p.name, p.port)
		}
		if owner, ok := seen[p.port]; ok {
			return fmt.Errorf("%s: port %d is already used by %s", p.name, p.port, owner)
		}
		seen[p.port] = p.name
	}

	switch strings.ToLower(c.Log.Level) {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
	default:
		return fmt.Errorf("log.level: unknown level %q (want %s, %s, %s or %s)",
			c.Log.Level, LevelDebug, LevelInfo, LevelWarn, LevelError)
	}

	if strings.TrimSpace(c.Startup.File) == "" {
		return errors.New("startup.file: must not be empty")
	}
	return nil
}
