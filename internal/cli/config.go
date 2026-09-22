package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/DisMosGit/triadsim/internal/config"
)

// newConfigCmd builds the config command group.
func newConfigCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the simulator configuration",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newConfigValidateCmd(out))
	return cmd
}

// newConfigValidateCmd builds config validate.
func newConfigValidateCmd(out io.Writer) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a YAML configuration file",
		Long: "Validate loads the file on top of the built-in defaults, rejects unknown keys\n" +
			"and checks every field, exactly the way start does before it listens.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if _, err := config.Load(file); err != nil {
				return err
			}
			_, err := fmt.Fprintf(out, "%s: OK\n", file)
			return err
		},
	}

	cmd.Flags().StringVar(&file, "file", config.DefaultPath, "path to the YAML configuration file")

	return cmd
}
