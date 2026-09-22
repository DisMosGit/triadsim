package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Version is the simulator version the version command reports. A release build
// can override it with -ldflags "-X github.com/DisMosGit/triadsim/internal/cli.Version=v0.1.0".
var Version = "0.1.0-dev"

// newVersionCmd builds the version command.
func newVersionCmd(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the simulator version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(out, "triadsim %s\n", Version)
			return err
		},
	}
}
