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
	"path/filepath"
)

// fileVersion is the schema version written by Save and accepted by Load.
const fileVersion = 1

// leaf kind names, one per supported Go leaf type.
const (
	kindBool    = "bool"
	kindInt     = "int"
	kindUint8   = "uint8"
	kindUint16  = "uint16"
	kindUint32  = "uint32"
	kindUint64  = "uint64"
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
	case uint8:
		return kindUint8, true
	case uint16:
		return kindUint16, true
	case uint32:
		return kindUint32, true
	case uint64:
		return kindUint64, true
	case float64:
		return kindFloat64, true
	case string:
		return kindString, true
	default:
		return "", false
	}
}

// Save writes values to path as the versioned startup document.
//
// The file is replaced atomically: the document is written to a temporary file
// in the same directory, flushed and renamed over path, so a crash, a kill or
// a full disk mid-write cannot leave a truncated document that the next boot
// refuses to load. Callers that need to serialize concurrent writers must do
// so themselves; Memory holds its lock across Save.
//
// An empty path, an unsupported value type or an I/O failure is an error; the
// file is not modified when a value is rejected.
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

	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("store: save %s: %w", path, err)
	}
	return nil
}

// writeFileAtomic replaces path with data through a temporary file in the same
// directory, an fsync of that file and a rename, then flushes the directory so
// the rename itself survives a crash. The temporary file is removed on every
// failure path.
func writeFileAtomic(path string, data []byte) error {
	// A directory is never a valid target; fail before touching anything so
	// the error does not depend on rename semantics.
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return errors.New("target is a directory")
	}

	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	discard := func(cause error) error {
		_ = file.Close()
		_ = os.Remove(tmp)
		return cause
	}

	if _, err := file.Write(data); err != nil {
		return discard(fmt.Errorf("write temporary file: %w", err))
	}
	if err := file.Sync(); err != nil {
		return discard(fmt.Errorf("sync temporary file: %w", err))
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename temporary file: %w", err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

// syncDir flushes the directory entry of a renamed file, so the rename itself
// survives a crash and not just the file contents.
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	return handle.Sync()
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
	case kindUint8:
		var value uint8
		if err := json.Unmarshal(leaf.Value, &value); err != nil {
			return nil, err
		}
		return value, nil
	case kindUint16:
		var value uint16
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
	case kindUint64:
		var value uint64
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
