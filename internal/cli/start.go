package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/config"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/log"
)

// newStartCmd builds the start command. Log records are written to out, which
// lets tests capture them instead of polluting stderr.
func newStartCmd(out io.Writer) *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the simulator",
		Long: "Start loads the YAML configuration, installs JSON logging on stderr and runs\n" +
			"until the process receives SIGINT or SIGTERM. The management planes are\n" +
			"added in later phases.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStart(cmd.Context(), configPath, out)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", config.DefaultPath, "path to the YAML configuration file")

	return cmd
}

// runStart loads the configuration, installs logging, creates the event bus and
// blocks until ctx is cancelled. Shutting down closes the bus, which closes
// every subscriber channel.
func runStart(ctx context.Context, configPath string, out io.Writer) error {
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

	logger.InfoContext(ctx, "simulator starting",
		"config", configPath,
		"snmp_port", cfg.SNMP.Port,
		"snmp_trap_port", cfg.SNMP.TrapPort,
		"netconf_port", cfg.NETCONF.Port,
		"restconf_port", cfg.RESTCONF.Port,
		"metrics_port", cfg.Metrics.Port,
		"gnmi_enabled", cfg.GNMI.Enabled,
		"log_level", cfg.Log.Level,
		"startup_file", cfg.Startup.File,
	)

	<-ctx.Done()

	reason := context.Cause(ctx)
	if reason == nil {
		reason = context.Canceled
	}
	logger.InfoContext(ctx, "simulator stopped", "reason", reason.Error())

	return nil
}
