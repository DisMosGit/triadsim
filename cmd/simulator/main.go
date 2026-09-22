// Command simulator is the single binary that runs the TriadSim device
// simulator.
package main

import (
	"log/slog"
	"os"

	"github.com/DisMosGit/triadsim/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		// Logging is configured by the command itself; when it failed before
		// that, slog falls back to its default stderr handler.
		slog.Error("simulator failed", "error", err)
		os.Exit(1)
	}
}
