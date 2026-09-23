package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/config"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/gnmi"
	"github.com/DisMosGit/triadsim/internal/l2"
	"github.com/DisMosGit/triadsim/internal/log"
	"github.com/DisMosGit/triadsim/internal/metrics"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/netconf"
	"github.com/DisMosGit/triadsim/internal/radio"
	"github.com/DisMosGit/triadsim/internal/restconf"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/snmp"
	"github.com/DisMosGit/triadsim/internal/store"
	syncd "github.com/DisMosGit/triadsim/internal/sync"
)

// shutdownTimeout bounds the HTTP shutdown when the context is cancelled.
const shutdownTimeout = 2 * time.Second

// uptimeInterval is how often the exposed sysUpTime leaf is refreshed.
const uptimeInterval = time.Second

// runtimeDeps overrides the listen addresses, the trap destination and the
// startup file, so tests can use ephemeral ports and a temporary file. An empty
// field falls back to the configuration.
type runtimeDeps struct {
	snmpAddr     string
	trapAddr     string
	netconfAddr  string
	restconfAddr string
	metricsAddr  string
	gnmiAddr     string
	startupFile  string
}

// newStartCmd builds the start command. Log records are written to out, which
// lets tests capture them instead of polluting stderr.
func newStartCmd(out io.Writer) *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the simulator",
		Long: "Start loads the YAML configuration, installs JSON logging on stderr and runs\n" +
			"the SNMP v2c agent, the NETCONF SSH subsystem, the RESTCONF HTTP server and\n" +
			"the Prometheus endpoint until the process receives SIGINT or SIGTERM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStart(cmd.Context(), configPath, out)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", config.DefaultPath, "path to the YAML configuration file")

	return cmd
}

// runStart is the production entry point: it uses the configured ports.
func runStart(ctx context.Context, configPath string, out io.Writer) error {
	return run(ctx, configPath, out, runtimeDeps{})
}

// run loads the configuration, builds the store, router and seed device, then
// serves SNMP and metrics until ctx is cancelled.
func run(ctx context.Context, configPath string, out io.Writer, deps runtimeDeps) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := log.New(cfg.Log.Level, out)
	if err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}
	log.SetDefault(logger)

	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()

	startupFile := deps.startupFile
	if startupFile == "" {
		startupFile = cfg.Startup.File
	}

	device := model.DefaultDevice()
	st := store.NewMemory(store.Options{StartupFile: startupFile})
	r, err := router.New(device, st)
	if err != nil {
		return fmt.Errorf("setup router: %w", err)
	}
	// The router hands the store the two schema-aware hooks: what a state
	// leaf is (kept out of the persisted startup document) and how to
	// validate a candidate or a loaded startup file.
	st.SetValidator(r.Validate)
	st.SetStateFilter(r.IsState)

	if err := st.LoadStartup(ctx); err != nil {
		return fmt.Errorf("load startup: %w", err)
	}
	if err := seedIfEmpty(ctx, st, r); err != nil {
		return fmt.Errorf("seed device: %w", err)
	}

	l2Manager, err := l2.New(l2.Deps{Router: r, Bus: bus, Clock: clock.RealClock{}})
	if err != nil {
		return fmt.Errorf("setup l2 domain: %w", err)
	}

	syncManager, err := syncd.New(syncd.Deps{Router: r, Bus: bus, Clock: clock.RealClock{}})
	if err != nil {
		return fmt.Errorf("setup sync domain: %w", err)
	}

	radioManager, err := radio.New(radio.Deps{Router: r, Bus: bus, Clock: clock.RealClock{}})
	if err != nil {
		return fmt.Errorf("setup radio domain: %w", err)
	}

	m := metrics.New(clock.RealClock{}, time.Now())
	go m.Run(ctx, bus.Subscribe())

	metricsAddr := deps.metricsAddr
	if metricsAddr == "" {
		metricsAddr = fmt.Sprintf(":%d", cfg.Metrics.Port)
	}
	metricsListener, err := net.Listen("tcp", metricsAddr)
	if err != nil {
		return fmt.Errorf("metrics listen %s: %w", metricsAddr, err)
	}
	server := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(metricsListener) }()

	agent := snmp.New(r, snmp.Options{
		Addr:      deps.snmpAddr,
		Port:      cfg.SNMP.Port,
		Community: snmp.DefaultCommunity,
		OnRequest: m.ObserveSNMPRequest,
	})
	if err := agent.Listen(); err != nil {
		_ = server.Close()
		return fmt.Errorf("setup snmp: %w", err)
	}
	defer func() { _ = agent.Close() }()

	trapSender := snmp.NewTrapSender(snmp.TrapOptions{
		Addr:      deps.trapAddr,
		Host:      cfg.SNMP.TrapHost,
		Port:      cfg.SNMP.TrapPort,
		Community: snmp.DefaultCommunity,
		Router:    r,
		Bus:       bus,
		Clock:     clock.RealClock{},
	})
	if err := trapSender.Listen(); err != nil {
		_ = server.Close()
		_ = agent.Close()
		return fmt.Errorf("setup snmp traps: %w", err)
	}
	defer func() { _ = trapSender.Close() }()

	netconfServer := netconf.New(r, st, bus, netconf.Options{
		Addr: deps.netconfAddr,
		Port: cfg.NETCONF.Port,
	})
	if err := netconfServer.Listen(); err != nil {
		_ = server.Close()
		return fmt.Errorf("setup netconf: %w", err)
	}
	defer func() { _ = netconfServer.Close() }()

	restconfServer := restconf.New(r, st, bus, restconf.Options{
		Addr:  deps.restconfAddr,
		Port:  cfg.RESTCONF.Port,
		Storm: l2Manager,
		Sync:  syncManager,
		Radio: radioManager,
	})
	if err := restconfServer.Listen(); err != nil {
		_ = server.Close()
		_ = agent.Close()
		_ = netconfServer.Close()
		return fmt.Errorf("setup restconf: %w", err)
	}
	defer func() { _ = restconfServer.Close() }()

	// The gNMI service is optional: it only listens when the configuration
	// enables it, and it keeps its own TLS-off listener like the other planes.
	var gnmiServer *gnmi.Server
	if cfg.GNMI.Enabled {
		gnmiServer = gnmi.New(r, st, bus, gnmi.Options{
			Addr:  deps.gnmiAddr,
			Port:  cfg.GNMI.Port,
			Clock: clock.RealClock{},
		})
		if err := gnmiServer.Listen(); err != nil {
			_ = server.Close()
			_ = agent.Close()
			_ = netconfServer.Close()
			_ = restconfServer.Close()
			return fmt.Errorf("setup gnmi: %w", err)
		}
		defer func() { _ = gnmiServer.Close() }()
	}

	serveErr := make(chan error, 4)
	go func() { serveErr <- agent.Serve(ctx) }()
	go func() { serveErr <- netconfServer.Serve(ctx) }()
	go func() { serveErr <- restconfServer.Serve(ctx) }()
	if gnmiServer != nil {
		go func() { serveErr <- gnmiServer.Serve(ctx) }()
	}
	go l2Manager.Run(ctx)
	go syncManager.Run(ctx)
	go radioManager.Run(ctx)
	go trapSender.Run(ctx)
	go runUptime(ctx, r)

	gnmiAddr := ""
	if gnmiServer != nil {
		gnmiAddr = gnmiServer.Addr().String()
	}
	logger.InfoContext(ctx, "simulator starting",
		"config", configPath,
		"device_id", device.SystemInfo.DeviceID,
		"snmp_port", cfg.SNMP.Port,
		"snmp_trap_host", cfg.SNMP.TrapHost,
		"snmp_trap_port", cfg.SNMP.TrapPort,
		"snmp_trap_dest", trapSender.Destination(),
		"netconf_port", cfg.NETCONF.Port,
		"restconf_port", cfg.RESTCONF.Port,
		"metrics_port", cfg.Metrics.Port,
		"gnmi_enabled", cfg.GNMI.Enabled,
		"gnmi_port", cfg.GNMI.Port,
		"gnmi_addr", gnmiAddr,
		"log_level", cfg.Log.Level,
		"startup_file", startupFile,
		"snmp_addr", agent.Addr().String(),
		"netconf_addr", netconfServer.Addr().String(),
		"restconf_addr", restconfServer.Addr().String(),
		"metrics_addr", metricsListener.Addr().String(),
	)

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil {
			shutdownServer(server)
			return err
		}
	}

	shutdownServer(server)

	reason := context.Cause(ctx)
	if reason == nil {
		reason = context.Canceled
	}
	logger.InfoContext(ctx, "simulator stopped", "reason", reason.Error())

	return nil
}

// seedIfEmpty writes the default device and commits it when the running
// datastore is empty, which is the case on the first boot.
func seedIfEmpty(ctx context.Context, st *store.Memory, r *router.Router) error {
	paths, err := st.List(ctx, store.Running, "")
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		return nil
	}
	if err := r.Seed(ctx, store.Candidate); err != nil {
		return err
	}
	return st.Commit(ctx)
}

// shutdownServer stops the metrics endpoint with a bounded fresh context, since
// the run context is already cancelled.
func shutdownServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = server.Shutdown(ctx)
}

// runUptime keeps the exposed system-info/uptime leaf in step with the wall
// clock, so the sysUpTime object and the trap timeticks report the same value.
// The leaf is state: SetState writes it to running and candidate, and the
// store's state filter keeps it out of the persisted startup document, so it
// is never persisted and cannot reappear stale after a restart.
func runUptime(ctx context.Context, r *router.Router) {
	start := time.Now()
	ticker := time.NewTicker(uptimeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			seconds := uint32(time.Since(start).Seconds())
			if _, err := r.SetState(ctx, "system-info/uptime", seconds); err != nil {
				slog.WarnContext(ctx, "uptime: writing system-info/uptime failed", "error", err)
			}
		}
	}
}
