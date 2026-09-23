package gnmi

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// testRouter builds a router over a seeded Memory store.
func testRouter(t *testing.T) (*router.Router, *store.Memory) {
	t.Helper()

	ctx := context.Background()
	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	st.SetStateFilter(r.IsState)
	require.NoError(t, r.Seed(ctx, store.Running))
	require.NoError(t, st.Rollback(ctx))
	return r, st
}

func TestNewFillsDefaults(t *testing.T) {
	_, st := testRouter(t)

	server := New(nil, st, nil, Options{Port: 9339})
	assert.Equal(t, ":9339", server.opts.Addr)
	assert.IsType(t, clock.RealClock{}, server.clock)

	explicit := New(nil, st, nil, Options{Addr: "127.0.0.1:0"})
	assert.Equal(t, "127.0.0.1:0", explicit.opts.Addr)
}

func TestListenRejectsMissingDependencies(t *testing.T) {
	r, st := testRouter(t)

	tests := map[string]*Server{
		"no router": New(nil, st, nil, Options{Addr: "127.0.0.1:0"}),
		"no store":  New(r, nil, nil, Options{Addr: "127.0.0.1:0"}),
	}

	for name, server := range tests {
		t.Run(name, func(t *testing.T) {
			require.Error(t, server.Listen())
		})
	}
}

func TestListenRejectsAnInvalidAddress(t *testing.T) {
	r, st := testRouter(t)

	server := New(r, st, nil, Options{Addr: "not-an-address"})
	require.Error(t, server.Listen())
	assert.Nil(t, server.Addr())
}

func TestServeBeforeListenFails(t *testing.T) {
	r, st := testRouter(t)

	server := New(r, st, nil, Options{Addr: "127.0.0.1:0"})
	require.Error(t, server.Serve(context.Background()))
}

func TestServeStopsWithTheContext(t *testing.T) {
	r, st := testRouter(t)

	server := New(r, st, nil, Options{Addr: "127.0.0.1:0"})
	require.NoError(t, server.Listen())
	require.NotNil(t, server.Addr())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "Serve must return once the context is cancelled")
	}
}

func TestServeReturnsWhenTheListenerCloses(t *testing.T) {
	r, st := testRouter(t)

	server := New(r, st, nil, Options{Addr: "127.0.0.1:0"})
	require.NoError(t, server.Listen())

	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background()) }()

	require.NoError(t, server.Close())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "Serve must return once the listener is closed")
	}
}

func TestCloseBeforeListenIsANoOp(t *testing.T) {
	r, st := testRouter(t)

	server := New(r, st, nil, Options{Addr: "127.0.0.1:0"})
	require.NoError(t, server.Close())
	assert.Nil(t, server.Addr())
}
