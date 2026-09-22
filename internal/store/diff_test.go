package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiffValues(t *testing.T) {
	tests := []struct {
		name      string
		running   map[string]any
		candidate map[string]any
		want      []Change
	}{
		{
			name:      "both empty",
			running:   map[string]any{},
			candidate: map[string]any{},
			want:      nil,
		},
		{
			name:      "equal",
			running:   map[string]any{"a/1": 1, "a/2": "two"},
			candidate: map[string]any{"a/1": 1, "a/2": "two"},
			want:      nil,
		},
		{
			name:      "create",
			running:   map[string]any{},
			candidate: map[string]any{"a/1": 1},
			want:      []Change{{Op: OpCreate, Path: "a/1", New: 1}},
		},
		{
			name:      "delete",
			running:   map[string]any{"a/1": 1},
			candidate: map[string]any{},
			want:      []Change{{Op: OpDelete, Path: "a/1", Old: 1}},
		},
		{
			name:      "update",
			running:   map[string]any{"a/1": 1},
			candidate: map[string]any{"a/1": 2},
			want:      []Change{{Op: OpUpdate, Path: "a/1", Old: 1, New: 2}},
		},
		{
			name:      "same value different type is an update",
			running:   map[string]any{"a/1": 1},
			candidate: map[string]any{"a/1": uint32(1)},
			want:      []Change{{Op: OpUpdate, Path: "a/1", Old: 1, New: uint32(1)}},
		},
		{
			name:      "mixed is sorted by path",
			running:   map[string]any{"b/1": 1, "a/2": 2},
			candidate: map[string]any{"a/1": 3, "b/1": 1},
			want: []Change{
				{Op: OpCreate, Path: "a/1", New: 3},
				{Op: OpDelete, Path: "a/2", Old: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diffValues(tt.running, tt.candidate)
			assert.Equal(t, tt.want, got)
		})
	}
}
