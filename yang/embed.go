// Package yang embeds the documentation-only YANG modules of the repository.
//
// The modules describe the same tree as the Go model in internal/model and are
// shipped inside the binary, so `simulator schema --yang` prints them without a
// checkout. The runtime never parses them: internal/model is the source of
// truth (see docs/adr/0002-model-vs-yang.md), and yang_test.go checks the two
// stay in step.
//
// The embed directive has to live next to the .yang files: go:embed rejects the
// ".." path elements that would be needed from internal/model.
package yang

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// extension is the file extension of every embedded module.
const extension = ".yang"

//go:embed *.yang
var files embed.FS

// Modules returns the file names of the embedded modules in sorted order.
func Modules() ([]string, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("yang: list modules: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), extension) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Read returns the source of one embedded module. The name may be a file name
// ("sim-sync.yang") or a module name ("sim-sync"); an unknown name is an error.
func Read(name string) ([]byte, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, errors.New("yang: module name is empty")
	}
	file := trimmed
	if !strings.HasSuffix(file, extension) {
		file += extension
	}

	source, err := files.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("yang: unknown module %q: %w", trimmed, err)
	}
	return source, nil
}
