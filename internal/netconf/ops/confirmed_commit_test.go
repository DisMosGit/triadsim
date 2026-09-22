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

// confirmedTimeout is the <confirm-timeout> used by the tests.
const confirmedTimeout = 30 * time.Second

// newConfirmedDeps returns dependencies whose confirmed-commit timers run on a
// FakeClock, plus that clock and the startup file commits persist to.
func newConfirmedDeps(t *testing.T) (Deps, *clock.FakeClock, string) {
	t.Helper()

	deps, startupFile := newTestDepsWithFile(t)
	fake := clock.NewFakeClock()
	deps.Confirmed = NewConfirmed(fake)
	return deps, fake, startupFile
}

// editCandidate writes tx-power to the candidate through the router, which
// validates it.
func editCandidate(t *testing.T, deps Deps, value float64) {
	t.Helper()
	_, err := deps.Router.Set(context.Background(), store.Candidate, txPowerPath, value)
	require.NoError(t, err)
}

func TestConfirmedCommitRevertsOnTimeout(t *testing.T) {
	ctx := context.Background()
	deps, fake, startupFile := newConfirmedDeps(t)
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	deps.Bus = bus
	events := bus.Subscribe()

	editCandidate(t, deps, 25.5)
	require.NoError(t, commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`))

	// The confirmed commit is applied immediately.
	assert.Equal(t, 25.5, storedValue(t, deps, store.Running, txPowerPath))
	assert.Equal(t, 25.5, storedValue(t, deps, store.Startup, txPowerPath))
	assert.True(t, deps.Confirmed.Pending())

	// An uncommitted change made after the confirmed commit survives the
	// rollback.
	editCandidate(t, deps, 26.0)

	fake.Advance(confirmedTimeout - time.Second)
	assert.Equal(t, 25.5, storedValue(t, deps, store.Running, txPowerPath), "the timeout has not expired yet")

	fake.Advance(time.Second)

	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
	assert.Equal(t, 20.0, storedValue(t, deps, store.Startup, txPowerPath))
	assert.Equal(t, 26.0, storedValue(t, deps, store.Candidate, txPowerPath))
	assert.False(t, deps.Confirmed.Pending())

	persisted, err := store.Load(ctx, startupFile)
	require.NoError(t, err)
	assert.Equal(t, 20.0, persisted[txPowerPath], "the rollback is persisted as startup")

	// The confirmed commit and its rollback are both announced.
	assert.Equal(t, "confirmed-commit", readConfigChanged(t, events))
	assert.Equal(t, "confirmed-commit rollback", readConfigChanged(t, events))
}

// readConfigChanged reads one ConfigChanged event and returns its message.
func readConfigChanged(t *testing.T, events <-chan event.Event) string {
	t.Helper()

	select {
	case got := <-events:
		require.Equal(t, event.TypeConfigChanged, got.Type)
		require.Equal(t, "device", got.Resource)
		require.False(t, got.Timestamp.IsZero(), "the bus stamps the event")
		return got.Message
	case <-time.After(5 * time.Second):
		t.Fatal("no ConfigChanged event was published")
		return ""
	}
}

func TestConfirmedCommitUsesDefaultTimeout(t *testing.T) {
	deps, fake, _ := newConfirmedDeps(t)

	editCandidate(t, deps, 25.5)
	require.NoError(t, commit(t, deps, `<commit><confirmed/></commit>`))

	fake.Advance(DefaultConfirmTimeout - time.Second)
	assert.Equal(t, 25.5, storedValue(t, deps, store.Running, txPowerPath))

	fake.Advance(time.Second)
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
}

func TestConfirmingCommitCancelsTheRollback(t *testing.T) {
	deps, fake, _ := newConfirmedDeps(t)

	editCandidate(t, deps, 25.5)
	require.NoError(t, commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`))
	require.NoError(t, commit(t, deps, `<commit/>`))

	assert.False(t, deps.Confirmed.Pending())

	fake.Advance(DefaultConfirmTimeout)
	assert.Equal(t, 25.5, storedValue(t, deps, store.Running, txPowerPath))
	assert.Equal(t, 25.5, storedValue(t, deps, store.Startup, txPowerPath))
}

func TestFollowUpConfirmedCommitExtendsTheTimeout(t *testing.T) {
	ctx := context.Background()
	deps, fake, startupFile := newConfirmedDeps(t)

	editCandidate(t, deps, 25.5)
	require.NoError(t, commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`))

	fake.Advance(20 * time.Second)

	// A follow-up confirmed commit applies its own changes but keeps the first
	// rollback target and only restarts the timer.
	editCandidate(t, deps, 26.0)
	require.NoError(t, commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`))
	assert.Equal(t, 26.0, storedValue(t, deps, store.Running, txPowerPath))

	fake.Advance(29 * time.Second)
	assert.Equal(t, 26.0, storedValue(t, deps, store.Running, txPowerPath), "the timer was reset")

	fake.Advance(time.Second)

	// Everything reverts to the state before the first confirmed commit.
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
	persisted, err := store.Load(ctx, startupFile)
	require.NoError(t, err)
	assert.Equal(t, 20.0, persisted[txPowerPath])
}

func TestConfirmedCommitFromAnotherSessionIsDenied(t *testing.T) {
	deps, fake, _ := newConfirmedDeps(t)

	editCandidate(t, deps, 25.5)
	require.NoError(t, commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`))

	// Another session may not start a second confirmed commit: the simulator
	// can only revert one unconfirmed configuration.
	deps.SessionID = 2
	editCandidate(t, deps, 30.0)

	err := commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`)

	require.Error(t, err)
	opErr := &Error{}
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, TagAccessDenied, opErr.Tag)
	assert.Equal(t, 25.5, storedValue(t, deps, store.Running, txPowerPath), "the refused commit must not be applied")

	fake.Advance(confirmedTimeout)
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
}

func TestSessionEndedRevertsConfirmedCommit(t *testing.T) {
	ctx := context.Background()
	deps, fake, startupFile := newConfirmedDeps(t)

	editCandidate(t, deps, 25.5)
	require.NoError(t, commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`))

	require.NoError(t, deps.Confirmed.SessionEnded(ctx, deps.SessionID))

	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
	assert.False(t, deps.Confirmed.Pending())

	persisted, err := store.Load(ctx, startupFile)
	require.NoError(t, err)
	assert.Equal(t, 20.0, persisted[txPowerPath])

	// The stopped timer must not revert anything a second time.
	fake.Advance(DefaultConfirmTimeout)
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
}

func TestSessionEndedLeavesAnotherSessionsConfirmedCommit(t *testing.T) {
	ctx := context.Background()
	deps, fake, _ := newConfirmedDeps(t)

	editCandidate(t, deps, 25.5)
	require.NoError(t, commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`))

	require.NoError(t, deps.Confirmed.SessionEnded(ctx, deps.SessionID+1))

	assert.True(t, deps.Confirmed.Pending())
	assert.Equal(t, 25.5, storedValue(t, deps, store.Running, txPowerPath))

	fake.Advance(confirmedTimeout)
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
}

func TestConfirmedCommitCloseStopsTheTimer(t *testing.T) {
	deps, fake, _ := newConfirmedDeps(t)

	editCandidate(t, deps, 25.5)
	require.NoError(t, commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`))

	deps.Confirmed.Close()

	assert.False(t, deps.Confirmed.Pending())
	fake.Advance(DefaultConfirmTimeout)
	assert.Equal(t, 25.5, storedValue(t, deps, store.Running, txPowerPath))
}

func TestConfirmedCommitWithoutManagerFails(t *testing.T) {
	deps, _ := newTestDepsWithFile(t)
	deps.Confirmed = nil

	editCandidate(t, deps, 25.5)
	err := commit(t, deps, `<commit><confirmed/></commit>`)

	require.Error(t, err)
	opErr := &Error{}
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, TagOperationFailed, opErr.Tag)
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
}

func TestConfirmedCommitRejectedCandidateArmsNothing(t *testing.T) {
	deps, fake, startupFile := newConfirmedDeps(t)
	ctx := context.Background()

	// Bypass the router to plant a value the model rejects.
	require.NoError(t, deps.Store.Set(ctx, store.Candidate, txPowerPath, 999.0))

	err := commit(t, deps, `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`)

	require.Error(t, err)
	opErr := &Error{}
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, TagInvalidValue, opErr.Tag)
	assert.False(t, deps.Confirmed.Pending(), "a rejected confirmed commit must not arm a rollback")

	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
	persisted, loadErr := store.Load(ctx, startupFile)
	require.NoError(t, loadErr)
	assert.Equal(t, 20.0, persisted[txPowerPath])

	fake.Advance(DefaultConfirmTimeout)
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
}
