package datatree_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/store"
)

// A canceled edit must leave the datastore untouched: the write phase is one
// atomic batch, so it cannot be interrupted half way through.
func TestApplyCanceledLeavesDatastoreUntouched(t *testing.T) {
	r, st := newTestTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := datatree.Apply(ctx, r, store.Candidate, datatree.Request{
		BasePath:  "vlans",
		Default:   datatree.OpMerge,
		Documents: []*datatree.Document{vlan(200, "VOICE")},
	})
	require.ErrorIs(t, err, context.Canceled)

	_, getErr := st.Get(context.Background(), store.Candidate, "vlans/vlan[id=200]/name")
	assert.ErrorIs(t, getErr, store.ErrNotFound)
}

// Concurrent edits of one list must not lose each other's entries: the whole
// read-modify-write runs inside one transaction.
func TestApplyConcurrentMergesKeepEveryEntry(t *testing.T) {
	ctx := context.Background()
	r, st := newTestTree(t)

	const workers = 8
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- datatree.Apply(ctx, r, store.Candidate, datatree.Request{
				BasePath:  "vlans",
				Default:   datatree.OpMerge,
				Documents: []*datatree.Document{vlan(uint16(200+i), "VLAN")},
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	for i := 0; i < workers; i++ {
		id := uint16(200 + i)
		path := fmt.Sprintf("vlans/vlan[id=%d]/name", id)
		stored, err := st.Get(ctx, store.Candidate, path)
		require.NoError(t, err, "vlan %d must survive the concurrent edits", id)
		assert.Equal(t, "VLAN", stored)
	}
}
