// Package cli wires the simulator's cobra commands.
//
// cmd/simulator/main.go only calls Execute; every flag and subcommand lives
// here so it can be exercised from tests without spawning a process.
package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// NewRootCmd returns the simulator root command with every subcommand
// attached.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "simulator",
		Short: "Simulate a telecom device with radio, L2 and sync domains",
		Long: "TriadSim simulates a single telecom device that combines a radio link (RRL),\n" +
			"L2 switching and synchronization domains on one managed device, exposing\n" +
			"SNMP v2c, NETCONF and RESTCONF management planes.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(newStartCmd(os.Stderr))

	return cmd
}

// Execute runs the root command with a context cancelled by SIGINT or SIGTERM,
// so long-running commands shut down cleanly.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return NewRootCmd().ExecuteContext(ctx)
}
