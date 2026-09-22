package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stub is a minimal implementation used to assert the interface stays
// implementable from outside the package.
type stub struct{}

func (stub) Get(context.Context, Datastore, string) (any, error) { return nil, ErrNotFound }
func (stub) Set(context.Context, Datastore, string, any) error   { return nil }
func (stub) Delete(context.Context, Datastore, string) error     { return ErrNotFound }
func (stub) List(context.Context, Datastore, string) ([]string, error) {
	return nil, nil
}
func (stub) Diff(context.Context) ([]Change, error) { return nil, nil }
func (stub) Commit(context.Context) error           { return nil }
func (stub) Rollback(context.Context) error         { return nil }
func (stub) Snapshot(context.Context) (map[string]any, error) {
	return nil, nil
}
func (stub) Restore(context.Context, map[string]any) error { return nil }

var _ Store = stub{}

func TestDatastoreValues(t *testing.T) {
	tests := []struct {
		name string
		ds   Datastore
		want string
	}{
		{name: "running", ds: Running, want: "running"},
		{name: "candidate", ds: Candidate, want: "candidate"},
		{name: "startup", ds: Startup, want: "startup"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.ds))
		})
	}
}

func TestOpValues(t *testing.T) {
	tests := []struct {
		name string
		op   Op
		want string
	}{
		{name: "create", op: OpCreate, want: "create"},
		{name: "update", op: OpUpdate, want: "update"},
		{name: "delete", op: OpDelete, want: "delete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.op))
		})
	}
}

func TestErrNotFoundIsComparable(t *testing.T) {
	_, err := stub{}.Get(context.Background(), Running, "interfaces/interface[name=radio0]")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestSentinelValues(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "not found", err: ErrNotFound, want: "store: path not found"},
		{name: "unknown datastore", err: ErrUnknownDatastore, want: "store: unknown datastore"},
		{name: "invalid path", err: ErrInvalidPath, want: "store: invalid path"},
		{name: "invalid value", err: ErrInvalidValue, want: "store: unsupported value type"},
		{name: "validation", err: ErrValidation, want: "store: candidate validation failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.EqualError(t, tt.err, tt.want)
		})
	}
}

func TestChangeJSON(t *testing.T) {
	tests := []struct {
		name   string
		change Change
		want   string
	}{
		{
			name:   "update carries both values",
			change: Change{Op: OpUpdate, Path: "a/b", Old: 10, New: 20},
			want:   `{"op":"update","path":"a/b","old":10,"new":20}`,
		},
		{
			name:   "create omits the old value",
			change: Change{Op: OpCreate, Path: "a/c", New: 20},
			want:   `{"op":"create","path":"a/c","new":20}`,
		},
		{
			name:   "delete omits the new value",
			change: Change{Op: OpDelete, Path: "a/d", Old: 20},
			want:   `{"op":"delete","path":"a/d","old":20}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.change)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}
