package router

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/store"
)

// A read-modify-write transaction must not lose an update: every increment
// reads and writes inside one transaction, so the final value is the number of
// increments. Without Transaction the read of one goroutine interleaves with
// the write of another and the counter comes out short.
func TestTransactionSerializesReadModifyWrite(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)
	const path = "mac-table/current-count"

	_, err := r.SetState(ctx, path, uint32(0))
	require.NoError(t, err)

	const workers = 16
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- r.Transaction(ctx, func() error {
				current, err := r.Get(ctx, store.Running, path)
				if err != nil {
					return err
				}
				count, ok := current.Value.(uint32)
				if !ok {
					return fmt.Errorf("current-count is %T, want uint32", current.Value)
				}
				_, err = r.SetState(ctx, path, count+1)
				return err
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	got, err := st.Get(ctx, store.Running, path)
	require.NoError(t, err)
	assert.Equal(t, uint32(workers), got)
}

// A canceled context must keep the transaction body from running at all.
func TestTransactionHonorsCanceledContext(t *testing.T) {
	r, _ := newTestRouter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := r.Transaction(ctx, func() error {
		called = true
		return nil
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, called, "the transaction body must not run for a canceled context")
}

// Apply is a single atomic batch: a batch the router rejects must leave the
// datastore untouched, including the entries that were valid on their own.
func TestApplyRejectsWholeBatch(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)

	err := r.Apply(ctx, store.Candidate, map[string]any{
		"vlans/vlan[id=200]/name":         "VOICE",
		"vlans/vlan[id=200]/no-such-leaf": "x",
	}, nil)
	require.Error(t, err)

	_, getErr := st.Get(ctx, store.Candidate, "vlans/vlan[id=200]/name")
	assert.ErrorIs(t, getErr, store.ErrNotFound, "the valid entry of a rejected batch must not be written")
}
