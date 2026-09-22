package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
	"github.com/DisMosGit/triadsim/yang"
)

// schemaNodeJSON is the JSON form of one node of the model schema tree.
type schemaNodeJSON struct {
	Name     string           `json:"name"`
	Path     string           `json:"path"`
	Module   string           `json:"module"`
	Kind     string           `json:"kind"`
	Key      string           `json:"key,omitempty"`
	Leaf     string           `json:"leaf,omitempty"`
	Children []schemaNodeJSON `json:"children,omitempty"`
}

// newSchemaCmd builds the schema command: the model schema tree by default, the
// embedded YANG modules with --yang.
func newSchemaCmd(out io.Writer) *cobra.Command {
	var (
		dumpYang   bool
		moduleName string
	)

	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the model schema or the embedded YANG modules",
		Long: "Schema prints the model schema tree — every node with the module that owns it,\n" +
			"its kind, the key of a list and the type of a leaf — as JSON. It is derived from\n" +
			"the Go model, so it needs no running simulator.\n\n" +
			"With --yang it prints the embedded YANG modules instead. The modules are\n" +
			"documentation: the runtime never parses them.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			switch {
			case dumpYang:
				return runSchemaYang(out, moduleName)
			case moduleName != "":
				return fmt.Errorf("--module needs --yang (try: schema --yang --module %s)", moduleName)
			default:
				return runSchemaTree(out)
			}
		},
	}

	cmd.Flags().BoolVar(&dumpYang, "yang", false,
		"print the embedded YANG modules instead of the schema tree")
	cmd.Flags().StringVar(&moduleName, "module", "",
		"with --yang, print exactly this module instead of all of them")

	return cmd
}

// runSchemaTree writes the model schema tree as indented JSON.
func runSchemaTree(out io.Writer) error {
	r, err := schemaRouter()
	if err != nil {
		return err
	}

	schema := schemaNodeJSON{
		Path:     "",
		Module:   router.ModuleDevice.Name,
		Kind:     string(router.KindContainer),
		Children: schemaChildren(r.Schema().Children, ""),
	}

	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("encode schema: %w", err)
	}
	_, err = fmt.Fprintf(out, "%s\n", encoded)
	return err
}

// schemaChildren converts the exported schema nodes, tracking the router path
// of each node so its owning module can be named.
func schemaChildren(nodes []router.SchemaNode, prefix string) []schemaNodeJSON {
	out := make([]schemaNodeJSON, 0, len(nodes))
	for _, node := range nodes {
		path := node.Name
		if prefix != "" {
			path = prefix + "/" + node.Name
		}

		entry := schemaNodeJSON{
			Name:   node.Name,
			Path:   path,
			Module: router.ModuleFor(path).Name,
			Kind:   string(node.Kind),
			Key:    node.Key,
		}
		if node.Kind == router.KindLeaf {
			entry.Leaf = string(node.Leaf)
		}
		entry.Children = schemaChildren(node.Children, path)
		out = append(out, entry)
	}
	return out
}

// runSchemaYang writes one or every embedded YANG module.
func runSchemaYang(out io.Writer, module string) error {
	if module != "" {
		source, err := yang.Read(module)
		if err != nil {
			return err
		}
		_, err = out.Write(withTrailingNewline(source))
		return err
	}

	names, err := yang.Modules()
	if err != nil {
		return err
	}
	for i, name := range names {
		source, err := yang.Read(name)
		if err != nil {
			return err
		}
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "// --- yang/%s ---\n", name); err != nil {
			return err
		}
		if _, err := out.Write(withTrailingNewline(source)); err != nil {
			return err
		}
	}
	return nil
}

// schemaRouter builds a router over the default device: the schema tree is
// type-driven, so the datastore stays empty.
func schemaRouter() (*router.Router, error) {
	r, err := router.New(model.DefaultDevice(), store.NewMemory(store.Options{}))
	if err != nil {
		return nil, fmt.Errorf("build schema: %w", err)
	}
	return r, nil
}

// withTrailingNewline returns source with exactly one trailing newline.
func withTrailingNewline(source []byte) []byte {
	return []byte(strings.TrimRight(string(source), "\n") + "\n")
}
