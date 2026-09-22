package netconf

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/netconf/ops"
)

// radioTxPowerPath is the leaf the tests edit to produce a configuration
// change.
const radioTxPowerPath = "interfaces/interface[name=radio0]/radio-link/tx-power"

// createSubscription subscribes one session to the default stream.
func createSubscription(t *testing.T, io *sessionIO, mode framingMode) {
	t.Helper()

	send(t, io, mode, `<rpc message-id="1"><create-subscription><stream>sim-events</stream></create-subscription></rpc>`)
	reply := readReply(t, io, mode)
	require.NotNil(t, reply.Child("ok"), "create-subscription must be accepted")
}

// readNotification reads the next NETCONF message of a session and requires it
// to be a <notification>.
func readNotification(t *testing.T, io *sessionIO, mode framingMode) *ops.Element {
	t.Helper()

	data, err := newMessageReader(io.reader, mode).ReadMessage()
	require.NoError(t, err)

	element, err := ops.ParseElement(bytes.TrimSpace(data))
	require.NoError(t, err)
	require.Equal(t, "notification", element.Name)
	return element
}

func TestCreateSubscriptionDeliversNotifications(t *testing.T) {
	tests := []struct {
		name      string
		mode      framingMode
		helloMode framingMode
		helloCaps []string
	}{
		{
			name:      "end-of-message",
			mode:      framingEOM,
			helloMode: framingEOM,
			helloCaps: []string{CapabilityBase10},
		},
		{
			name:      "chunked",
			mode:      framingChunked,
			helloMode: framingChunked,
			helloCaps: []string{CapabilityBase11, CapabilityBase10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, st := newTestStore(t)
			bus := event.New(event.DefaultBuffer, clock.RealClock{})
			defer bus.Close()
			srv := startTestServer(t, r, st, bus)

			io, hello := openNetconfSession(t, dialSSH(t, srv.Addr().String()))
			assert.Contains(t, hello.Capabilities, CapabilityNotification)
			sendHello(t, io, tt.helloMode, tt.helloCaps...)
			createSubscription(t, io, tt.mode)
			assert.Equal(t, 1, srv.dispatcher.Subscribers())

			// The event bus carries an event with an explicit timestamp, so the
			// notification document is deterministic.
			bus.Publish(event.Event{
				Type:      event.TypeAlarmRaised,
				Resource:  "radio0",
				Severity:  "major",
				Message:   "radio link down",
				Timestamp: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			})

			notification := readNotification(t, io, tt.mode)
			assert.Equal(t, "2024-01-02T03:04:05Z", notification.Child("eventTime").TrimmedText())

			payload := notification.Child("event")
			require.NotNil(t, payload)
			assert.Equal(t, "AlarmRaised", payload.Child("type").TrimmedText())
			assert.Equal(t, "radio0", payload.Child("resource").TrimmedText())
			assert.Equal(t, "major", payload.Child("severity").TrimmedText())
			assert.Equal(t, "radio link down", payload.Child("message").TrimmedText())
		})
	}
}

func TestCreateSubscriptionWithoutBusIsRejected(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)

	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))
	sendHello(t, io, framingEOM, CapabilityBase10)

	send(t, io, framingEOM, `<rpc message-id="1"><create-subscription/></rpc>`)

	assert.Equal(t, ops.TagOperationNotSupported, errorTag(t, readReply(t, io, framingEOM)))
}

func TestCreateSubscriptionRejectsUnknownStream(t *testing.T) {
	r, st := newTestStore(t)
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	srv := startTestServer(t, r, st, bus)

	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))
	sendHello(t, io, framingEOM, CapabilityBase10)

	send(t, io, framingEOM, `<rpc message-id="1"><create-subscription><stream>NETCONF</stream></create-subscription></rpc>`)

	assert.Equal(t, ops.TagInvalidValue, errorTag(t, readReply(t, io, framingEOM)))
	assert.Equal(t, 0, srv.dispatcher.Subscribers())
}

func TestSubscriptionEndsWithTheSession(t *testing.T) {
	r, st := newTestStore(t)
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	srv := startTestServer(t, r, st, bus)

	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))
	sendHello(t, io, framingEOM, CapabilityBase10)
	createSubscription(t, io, framingEOM)

	send(t, io, framingEOM, `<rpc message-id="2"><close-session/></rpc>`)
	require.NotNil(t, readReply(t, io, framingEOM).Child("ok"))
	expectSessionClosed(t, io)

	assert.Equal(t, 0, srv.dispatcher.Subscribers())
}

func TestCommitNotificationReachesASubscribedSession(t *testing.T) {
	r, st := newTestStore(t)
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	srv := startTestServer(t, r, st, bus)
	client := dialSSH(t, srv.Addr().String())

	// One session subscribes and then only listens.
	subscriber, _ := openNetconfSession(t, client)
	sendHello(t, subscriber, framingEOM, CapabilityBase10)
	createSubscription(t, subscriber, framingEOM)

	// Another session commits a change.
	committer, _ := openNetconfSession(t, client)
	sendHello(t, committer, framingEOM, CapabilityBase10)
	send(t, committer, framingEOM, `<rpc message-id="1"><edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>radio0</name><radio-link><tx-power>24</tx-power></radio-link>`+
		`</interface></interfaces></config></edit-config></rpc>`)
	require.NotNil(t, readReply(t, committer, framingEOM).Child("ok"))
	send(t, committer, framingEOM, `<rpc message-id="2"><commit/></rpc>`)
	require.NotNil(t, readReply(t, committer, framingEOM).Child("ok"))

	// The subscriber receives the ConfigChanged event of the commit.
	notification := readNotification(t, subscriber, framingEOM)
	assert.NotEmpty(t, notification.Child("eventTime").TrimmedText())

	payload := notification.Child("event")
	require.NotNil(t, payload)
	assert.Equal(t, "ConfigChanged", payload.Child("type").TrimmedText())
	assert.Equal(t, "device", payload.Child("resource").TrimmedText())
	assert.Equal(t, "commit", payload.Child("message").TrimmedText())
}
