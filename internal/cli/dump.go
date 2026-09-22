package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/restconf"
)

// dumpContents are the ?content= selections dump accepts.
var dumpContents = []string{
	string(datatree.ContentConfig),
	string(datatree.ContentNonConfig),
	string(datatree.ContentAll),
}

// newDumpCmd builds the dump command.
func newDumpCmd(out io.Writer) *cobra.Command {
	var format, content, addr string

	cmd := &cobra.Command{
		Use:   "dump",
		Short: "Dump the datastore of a running simulator",
		Long: "Dump fetches the datastore of a running simulator over RESTCONF and prints\n" +
			"it in the requested media type.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDump(cmd.Context(), out, addr, format, content)
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json or xml")
	cmd.Flags().StringVar(&content, "content", string(datatree.ContentAll),
		"datastore content to dump: "+strings.Join(dumpContents, ", "))
	cmd.Flags().StringVar(&addr, "addr", DefaultSimulatorAddr, "base URL of the running simulator")

	return cmd
}

// runDump fetches the datastore and writes it to out.
func runDump(ctx context.Context, out io.Writer, addr, format, content string) error {
	accept, ok := dumpMediaType(format)
	if !ok {
		return fmt.Errorf("unknown format %q (want json or xml)", format)
	}
	if !slices.Contains(dumpContents, content) {
		return fmt.Errorf("unknown content %q (want %s)", content, strings.Join(dumpContents, ", "))
	}

	target := endpoint(addr, "/restconf/data") + "?content=" + url.QueryEscape(content)
	body, err := get(ctx, target, accept)
	if err != nil {
		return fmt.Errorf("dump: %w", err)
	}

	if format == "xml" {
		_, err = fmt.Fprintln(out, body)
		return err
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(body), "", "  "); err != nil {
		return fmt.Errorf("dump: decode response: %w", err)
	}
	_, err = fmt.Fprintln(out, pretty.String())
	return err
}

// dumpMediaType maps an output format onto the RESTCONF media type and the
// Accept header that asks for it.
func dumpMediaType(format string) (string, bool) {
	switch format {
	case "json":
		return restconf.MediaTypeJSON, true
	case "xml":
		return restconf.MediaTypeXML, true
	default:
		return "", false
	}
}
