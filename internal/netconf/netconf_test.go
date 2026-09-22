package netconf

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// newTestStore returns a router and a store seeded with DefaultDevice. The
// startup file lives in the test's temporary directory, so commits persist
// without touching the repository.
func newTestStore(t *testing.T) (*router.Router, *store.Memory) {
	t.Helper()
	ctx := context.Background()

	st := store.NewMemory(store.Options{StartupFile: filepath.Join(t.TempDir(), "startup.json")})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)

	require.NoError(t, r.Seed(ctx, store.Candidate))
	require.NoError(t, st.Commit(ctx))
	return r, st
}

// startTestServer listens on an ephemeral loopback port and serves until the
// test ends.
func startTestServer(t *testing.T, r *router.Router, st store.Store, bus *event.Bus) *Server {
	t.Helper()

	srv := New(r, st, bus, Options{Addr: "127.0.0.1:0"})
	require.NoError(t, srv.Listen())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("netconf server did not stop")
		}
	})
	return srv
}
