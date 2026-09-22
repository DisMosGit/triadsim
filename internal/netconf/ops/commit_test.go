package ops

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/store"
)

// commit runs <commit> over the given operation body.
func commit(t *testing.T, deps Deps, body string) error {
	t.Helper()
	return Commit(context.Background(), deps, parseOperation(t, body))
}

func TestCommitAppliesCandidateAndPersistsStartup(t *testing.T) {
	deps, startupFile := newTestDepsWithFile(t)
	ctx := context.Background()

	_, err := deps.Router.Set(ctx, store.Candidate, txPowerPath, 25.5)
	require.NoError(t, err)

	require.NoError(t, commit(t, deps, `<commit/>`))

	assert.Equal(t, 25.5, storedValue(t, deps, store.Running, txPowerPath))
	assert.Equal(t, 25.5, storedValue(t, deps, store.Startup, txPowerPath))

	// The commit is persisted to the startup file.
	persisted, err := store.Load(ctx, startupFile)
	require.NoError(t, err)
	assert.Equal(t, 25.5, persisted[txPowerPath])
}

func TestCommitRejectsAnInvalidCandidateWithoutChangingAnything(t *testing.T) {
	deps, startupFile := newTestDepsWithFile(t)
	ctx := context.Background()

	// Bypass the router to plant a value the model rejects.
	require.NoError(t, deps.Store.Set(ctx, store.Candidate, txPowerPath, 999.0))

	err := commit(t, deps, `<commit/>`)

	require.Error(t, err)
	opErr := &Error{}
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, TagInvalidValue, opErr.Tag)
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))

	persisted, err := store.Load(ctx, startupFile)
	require.NoError(t, err)
	assert.Equal(t, 20.0, persisted[txPowerPath])
}

func TestCommitPublishesConfigChanged(t *testing.T) {
	deps := newTestDeps(t)
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	deps.Bus = bus
	events := bus.Subscribe()

	require.NoError(t, commit(t, deps, `<commit/>`))

	select {
	case got := <-events:
		assert.Equal(t, event.TypeConfigChanged, got.Type)
		assert.Equal(t, "device", got.Resource)
		assert.False(t, got.Timestamp.IsZero(), "the bus stamps the event")
	case <-time.After(5 * time.Second):
		t.Fatal("commit must publish ConfigChanged")
	}
}

func TestCommitWithoutBusIsSafe(t *testing.T) {
	deps := newTestDeps(t)
	deps.Bus = nil

	assert.NoError(t, commit(t, deps, `<commit/>`))
}

func TestCommitAcceptsCandidateSourceAndRejectsTheRest(t *testing.T) {
	deps := newTestDeps(t)

	require.NoError(t, commit(t, deps, `<commit><source><candidate/></source></commit>`))

	tests := []struct {
		name string
		body string
		tag  string
	}{
		{name: "running source", body: `<commit><source><running/></source></commit>`, tag: TagOperationNotSupported},
		{name: "startup source", body: `<commit><source><startup/></source></commit>`, tag: TagOperationNotSupported},
		{name: "confirmed", body: `<commit><confirmed/></commit>`, tag: TagOperationNotSupported},
		{name: "confirm-timeout", body: `<commit><confirm-timeout>30</confirm-timeout></commit>`, tag: TagOperationNotSupported},
		{name: "persist", body: `<commit><persist>token</persist></commit>`, tag: TagOperationNotSupported},
		{name: "persist-id", body: `<commit><persist-id>token</persist-id></commit>`, tag: TagOperationNotSupported},
		{name: "unknown source", body: `<commit><source><operational/></source></commit>`, tag: TagUnknownElement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := commit(t, deps, tt.body)

			require.Error(t, err)
			opErr := &Error{}
			require.ErrorAs(t, err, &opErr)
			assert.Equal(t, tt.tag, opErr.Tag)
		})
	}
}

func TestDiscardChangesRestoresCandidate(t *testing.T) {
	ctx := context.Background()
	deps := newTestDeps(t)

	_, err := deps.Router.Set(ctx, store.Candidate, txPowerPath, 25.5)
	require.NoError(t, err)
	require.NoError(t, DiscardChanges(ctx, deps, parseOperation(t, `<discard-changes/>`)))

	assert.Equal(t, 20.0, storedValue(t, deps, store.Candidate, txPowerPath))
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
}

func TestEditCommitDiscardCycle(t *testing.T) {
	deps := newTestDeps(t)
	target := `<target><candidate/></target>`
	edit := `<edit-config>` + target + `<config><interfaces><interface><name>radio0</name><radio-link>` +
		`<tx-power>24</tx-power></radio-link></interface></interfaces></config></edit-config>`

	// Edit, discard, edit, commit.
	require.NoError(t, editConfig(t, deps, edit))
	require.NoError(t, DiscardChanges(context.Background(), deps, parseOperation(t, `<discard-changes/>`)))
	assert.Equal(t, 20.0, storedValue(t, deps, store.Candidate, txPowerPath))

	require.NoError(t, editConfig(t, deps, edit))
	require.NoError(t, commit(t, deps, `<commit/>`))
	assert.Equal(t, 24.0, storedValue(t, deps, store.Running, txPowerPath))
	assert.Equal(t, 24.0, storedValue(t, deps, store.Candidate, txPowerPath))
}
