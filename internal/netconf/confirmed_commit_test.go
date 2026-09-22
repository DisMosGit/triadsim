package netconf

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/store"
)

// runningTxPower reads the running tx-power of radio0.
func runningTxPower(t *testing.T, st *store.Memory) any {
	t.Helper()

	value, err := st.Get(context.Background(), store.Running, radioTxPowerPath)
	require.NoError(t, err)
	return value
}

// editTxPower writes tx-power to the candidate over an open session.
func editTxPower(t *testing.T, io *sessionIO, messageID, value string) {
	t.Helper()

	send(t, io, framingEOM, `<rpc message-id="`+messageID+`"><edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>radio0</name><radio-link><tx-power>`+value+`</tx-power></radio-link>`+
		`</interface></interfaces></config></edit-config></rpc>`)
	require.NotNil(t, readReply(t, io, framingEOM).Child("ok"))
}

// confirmedCommitSession opens a session, edits tx-power to 24 and commits it
// with <confirmed/> and a 30 second timeout.
func confirmedCommitSession(t *testing.T, addr string) *sessionIO {
	t.Helper()

	io, _ := openNetconfSession(t, dialSSH(t, addr))
	sendHello(t, io, framingEOM, CapabilityBase10)

	editTxPower(t, io, "1", "24")
	send(t, io, framingEOM, `<rpc message-id="2"><commit><confirmed/><confirm-timeout>30</confirm-timeout></commit></rpc>`)
	require.NotNil(t, readReply(t, io, framingEOM).Child("ok"))

	return io
}

func TestConfirmedCommitRollsBackOnTimeout(t *testing.T) {
	r, st := newTestStore(t)
	fake := clock.NewFakeClock()
	srv := startTestServerOptions(t, r, st, nil, Options{Clock: fake})

	io, hello := openNetconfSession(t, dialSSH(t, srv.Addr().String()))
	assert.Contains(t, hello.Capabilities, CapabilityConfirmedCommit)
	sendHello(t, io, framingEOM, CapabilityBase10)
	editTxPower(t, io, "1", "24")

	send(t, io, framingEOM, `<rpc message-id="2"><commit><confirmed/><confirm-timeout>30</confirm-timeout></commit></rpc>`)
	require.NotNil(t, readReply(t, io, framingEOM).Child("ok"))

	// The confirmed commit is applied immediately and armed on the injected
	// clock.
	assert.Equal(t, 24.0, runningTxPower(t, st))
	assert.True(t, srv.confirmed.Pending())

	fake.Advance(29 * time.Second)
	assert.Equal(t, 24.0, runningTxPower(t, st))

	fake.Advance(time.Second)
	assert.Equal(t, 20.0, runningTxPower(t, st), "the timeout must revert the configuration")
	assert.False(t, srv.confirmed.Pending())
}

func TestConfirmingCommitKeepsTheConfiguration(t *testing.T) {
	r, st := newTestStore(t)
	fake := clock.NewFakeClock()
	srv := startTestServerOptions(t, r, st, nil, Options{Clock: fake})

	io := confirmedCommitSession(t, srv.Addr().String())

	send(t, io, framingEOM, `<rpc message-id="3"><commit/></rpc>`)
	require.NotNil(t, readReply(t, io, framingEOM).Child("ok"))
	assert.False(t, srv.confirmed.Pending())

	fake.Advance(10 * time.Minute)
	assert.Equal(t, 24.0, runningTxPower(t, st))
}

func TestConfirmedCommitRollsBackWhenTheSessionEnds(t *testing.T) {
	r, st := newTestStore(t)
	fake := clock.NewFakeClock()
	srv := startTestServerOptions(t, r, st, nil, Options{Clock: fake})

	io := confirmedCommitSession(t, srv.Addr().String())
	assert.Equal(t, 24.0, runningTxPower(t, st))

	// close-session ends the session, and the server reverts the unconfirmed
	// configuration before it closes the channel, so the rollback is already
	// done once the client sees end of stream.
	send(t, io, framingEOM, `<rpc message-id="3"><close-session/></rpc>`)
	require.NotNil(t, readReply(t, io, framingEOM).Child("ok"))
	expectSessionClosed(t, io)

	assert.Equal(t, 20.0, runningTxPower(t, st), "an unconfirmed commit must not survive its session")
	assert.False(t, srv.confirmed.Pending())

	fake.Advance(10 * time.Minute)
	assert.Equal(t, 20.0, runningTxPower(t, st))
}

func TestConfirmedCommitOfAnotherSessionIsDenied(t *testing.T) {
	r, st := newTestStore(t)
	fake := clock.NewFakeClock()
	srv := startTestServerOptions(t, r, st, nil, Options{Clock: fake})

	// The first session owns the confirmed commit in progress.
	confirmedCommitSession(t, srv.Addr().String())

	// A second session may not start its own.
	second, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))
	sendHello(t, second, framingEOM, CapabilityBase10)
	send(t, second, framingEOM, `<rpc message-id="1"><commit><confirmed/></commit></rpc>`)

	assert.Equal(t, "access-denied", errorTag(t, readReply(t, second, framingEOM)))

	// The first session's rollback is unaffected.
	fake.Advance(30 * time.Second)
	assert.Equal(t, 20.0, runningTxPower(t, st))
}
