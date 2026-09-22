package gnmi

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DisMosGit/triadsim/internal/event"
)

// subscribeRequest builds the first request of a subscription stream.
func subscribeRequest(list *gnmi.SubscriptionList) *gnmi.SubscribeRequest {
	return &gnmi.SubscribeRequest{Request: &gnmi.SubscribeRequest_Subscribe{Subscribe: list}}
}

// openStream opens a subscription stream bound to the test's lifetime.
func openStream(ctx context.Context, t *testing.T, ts *testServer, list *gnmi.SubscriptionList) gnmi.GNMI_SubscribeClient {
	t.Helper()

	stream, err := ts.client.Subscribe(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(subscribeRequest(list)))
	return stream
}

// subscription builds one ON_CHANGE subscription element.
func subscription(path *gnmi.Path) *gnmi.Subscription {
	return &gnmi.Subscription{Path: path, Mode: gnmi.SubscriptionMode_ON_CHANGE}
}

func TestSubscribeOnceSendsSnapshotThenSync(t *testing.T) {
	ts := newTestServer(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream := openStream(ctx, t, ts, &gnmi.SubscriptionList{
		Mode:         gnmi.SubscriptionList_ONCE,
		Encoding:     gnmi.Encoding_JSON_IETF,
		Subscription: []*gnmi.Subscription{subscription(path(elem("ptp")))},
	})

	first, err := stream.Recv()
	require.NoError(t, err)
	notification := first.GetUpdate()
	require.NotNil(t, notification)
	assert.Equal(t, "ptp", relativeString(notification.GetPrefix()))
	assert.NotNil(t, updateWith(notification, "clock", "state"))

	second, err := stream.Recv()
	require.NoError(t, err)
	assert.True(t, second.GetSyncResponse())

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestSubscribeOnChangeFollowsTheEventBus(t *testing.T) {
	ts := newTestServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	link := path(elem("interfaces"), keyed("interface", "name", "radio0"), elem("radio-link"))
	stream := openStream(ctx, t, ts, &gnmi.SubscriptionList{
		Mode:         gnmi.SubscriptionList_STREAM,
		Encoding:     gnmi.Encoding_PROTO,
		UpdatesOnly:  true,
		Subscription: []*gnmi.Subscription{subscription(link)},
	})

	// UpdatesOnly still ends with a sync_response marker.
	sync, err := stream.Recv()
	require.NoError(t, err)
	require.True(t, sync.GetSyncResponse())

	// An event about another subsystem must be filtered out.
	ts.bus.Publish(event.Event{
		Type:     event.TypeStateTransition,
		Resource: "ptp/clock",
		From:     "locked",
		To:       "holdover-in-spec",
	})
	// A radio alarm about the subscribed link must arrive.
	ts.bus.Publish(event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "radio0",
		Severity: "critical",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	})

	response, err := stream.Recv()
	require.NoError(t, err)
	notification := response.GetUpdate()
	require.NotNil(t, notification)
	assert.Equal(t, "interfaces/interface[name=radio0]/radio-link",
		relativeString(notification.GetPrefix()))
	rssi := updateWith(notification, "rssi")
	require.NotNil(t, rssi, "the notification must carry the link's state leaves")
	assert.InDelta(t, -72.5, rssi.GetVal().GetDoubleVal(), 0.001)
}

func TestSubscribeOnChangeNotifiesAnOuterSubscription(t *testing.T) {
	ts := newTestServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	outer := path(elem("interfaces"), keyed("interface", "name", "radio0"))
	stream := openStream(ctx, t, ts, &gnmi.SubscriptionList{
		Mode:         gnmi.SubscriptionList_STREAM,
		UpdatesOnly:  true,
		Subscription: []*gnmi.Subscription{subscription(outer)},
	})
	_, err := stream.Recv()
	require.NoError(t, err)

	ts.bus.Publish(event.Event{Type: event.TypeAlarmRaised, Resource: "radio0"})

	response, err := stream.Recv()
	require.NoError(t, err)
	notification := response.GetUpdate()
	require.NotNil(t, notification)
	assert.Equal(t, "interfaces/interface[name=radio0]", relativeString(notification.GetPrefix()))
	assert.NotNil(t, updateWith(notification, "radio-link", "rssi"))
}

func TestSubscribeSendsTheInitialSnapshot(t *testing.T) {
	ts := newTestServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream := openStream(ctx, t, ts, &gnmi.SubscriptionList{
		Mode:         gnmi.SubscriptionList_STREAM,
		Subscription: []*gnmi.Subscription{subscription(path(elem("ptp")))},
	})

	snapshot, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, snapshot.GetUpdate())
	assert.NotNil(t, updateWith(snapshot.GetUpdate(), "clock", "domain"))

	sync, err := stream.Recv()
	require.NoError(t, err)
	assert.True(t, sync.GetSyncResponse())
}

func TestSubscribeRejectsUnsupportedModes(t *testing.T) {
	ts := newTestServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tests := map[string]*gnmi.SubscriptionList{
		"sample subscription": {
			Mode: gnmi.SubscriptionList_STREAM,
			Subscription: []*gnmi.Subscription{{
				Path: path(elem("ptp")),
				Mode: gnmi.SubscriptionMode_SAMPLE,
			}},
		},
		"poll subscription list": {Mode: gnmi.SubscriptionList_POLL},
	}

	for name, list := range tests {
		t.Run(name, func(t *testing.T) {
			stream := openStream(ctx, t, ts, list)
			_, err := stream.Recv()
			require.Error(t, err)
			assert.Equal(t, codes.Unimplemented, status.Code(err))
		})
	}
}

func TestSubscribePollRequestIsUnimplemented(t *testing.T) {
	ts := newTestServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := ts.client.Subscribe(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&gnmi.SubscribeRequest{
		Request: &gnmi.SubscribeRequest_Poll{Poll: &gnmi.Poll{}},
	}))

	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestSubscribeUnknownPathIsNotFound(t *testing.T) {
	ts := newTestServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream := openStream(ctx, t, ts, &gnmi.SubscriptionList{
		Mode:         gnmi.SubscriptionList_ONCE,
		Subscription: []*gnmi.Subscription{subscription(path(elem("no-such-container")))},
	})

	_, err := stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestSubscribeOnChangeNeedsABus(t *testing.T) {
	ts := newTestServer(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream := openStream(ctx, t, ts, &gnmi.SubscriptionList{
		Mode:         gnmi.SubscriptionList_STREAM,
		Subscription: []*gnmi.Subscription{subscription(path(elem("ptp")))},
	})

	_, err := stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestSubscribeStopsWhenTheClientGoesAway(t *testing.T) {
	ts := newTestServer(t, true)
	ctx, cancel := context.WithCancel(context.Background())

	stream := openStream(ctx, t, ts, &gnmi.SubscriptionList{
		Mode:         gnmi.SubscriptionList_STREAM,
		UpdatesOnly:  true,
		Subscription: []*gnmi.Subscription{subscription(path(elem("ptp")))},
	})
	_, err := stream.Recv()
	require.NoError(t, err)

	cancel()
	require.Eventually(t, func() bool {
		_, err := stream.Recv()
		return err != nil && !errors.Is(err, io.EOF)
	}, 2*time.Second, 10*time.Millisecond)
}
