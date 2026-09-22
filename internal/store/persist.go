// Persistence of the startup datastore.
//
// The startup datastore is written as a versioned JSON document whose leaves
// carry an explicit kind. Plain JSON would decode every number as float64 and
// silently change the type of an int or uint32 leaf across a restart, so the
// kind is stored next to the value and used when loading.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// fileVersion is the schema version written by Save and accepted by Load.
const fileVersion = 1

// leaf kind names, one per supported Go leaf type.
const (
	kindBool    = "bool"
	kindInt     = "int"
	kindUint32  = "uint32"
	kindFloat64 = "float64"
	kindString  = "string"
)

// diskFile is the startup.json document.
type diskFile struct {
	Version int                 `json:"version"`
	Values  map[string]diskLeaf `json:"values"`
}

// diskLeaf is one path's value on disk. Value is decoded according to Kind.
type diskLeaf struct {
	Kind  string          `json:"kind"`
	Value json.RawMessage `json:"value"`
}

// leafKindOf reports the on-disk kind of a supported leaf value. It returns
// false for every other Go type.
func leafKindOf(value any) (string, bool) {
	switch value.(type) {
	case bool:
		return kindBool, true
	case int:
		return kindInt, true
	case uint32:
		return kindUint32, true
	case float64:
		return kindFloat64, true
	case string:
		return kindString, true
	default:
		return "", false
	}
}

// Save writes values to path as the versioned startup document, using
// os.WriteFile with mode 0o600. An empty path, an unsupported value type or an
// I/O failure is an error; the file is not modified when a value is rejected.
func Save(ctx context.Context, path string, values map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == "" {
		return errors.New("store: save: path is empty")
	}

	doc := diskFile{Version: fileVersion, Values: make(map[string]diskLeaf, len(values))}
	for p, value := range values {
		kind, ok := leafKindOf(value)
		if !ok {
			return fmt.Errorf("store: save %s: %w: %T", p, ErrInvalidValue, value)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("store: save %s: %w", p, err)
		}
		doc.Values[p] = diskLeaf{Kind: kind, Value: raw}
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("store: save %s: %w", path, err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("store: save %s: %w", path, err)
	}
	return nil
}

// Load reads the startup document at path. A missing file yields an empty map
// and no error, because the first boot has no startup file yet. Malformed
// JSON, an unsupported version, an unknown kind or a value that does not
// decode into its kind is an error.
func Load(ctx context.Context, path string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("store: load: path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("store: load %s: %w", path, err)
	}

	var doc diskFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("store: load %s: %w", path, err)
	}
	if doc.Version != fileVersion {
		return nil, fmt.Errorf("store: load %s: unsupported version %d", path, doc.Version)
	}

	values := make(map[string]any, len(doc.Values))
	for p, leaf := range doc.Values {
		value, err := decodeLeaf(leaf)
		if err != nil {
			return nil, fmt.Errorf("store: load %s: %s: %w", path, p, err)
		}
		values[p] = value
	}
	return values, nil
}

// decodeLeaf turns one on-disk leaf back into its typed Go value.
func decodeLeaf(leaf diskLeaf) (any, error) {
	switch leaf.Kind {
	case kindBool:
		var value bool
		if err := json.Unmarshal(leaf.Value, &value); err != nil {
			return nil, err
		}
		return value, nil
	case kindInt:
		var value int
		if err := json.Unmarshal(leaf.Value, &value); err != nil {
			return nil, err
		}
		return value, nil
	case kindUint32:
		var value uint32
		if err := json.Unmarshal(leaf.Value, &value); err != nil {
			return nil, err
		}
		return value, nil
	case kindFloat64:
		var value float64
		if err := json.Unmarshal(leaf.Value, &value); err != nil {
			return nil, err
		}
		return value, nil
	case kindString:
		var value string
		if err := json.Unmarshal(leaf.Value, &value); err != nil {
			return nil, err
		}
		return value, nil
	default:
		return nil, fmt.Errorf("%w: kind %q", ErrInvalidValue, leaf.Kind)
	}
}
