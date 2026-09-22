package notif

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/netconf/ops"
)

// parseOperation parses one operation element from its XML text.
func parseOperation(t *testing.T, body string) *ops.Element {
	t.Helper()

	element, err := ops.ParseElement([]byte(body))
	require.NoError(t, err)
	return element
}

// captureLogs installs a JSON logger writing into a buffer as the slog default
// for the duration of the test and returns that buffer.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &buf
}

// collector returns a Sender and the channel it posts every document to. The
// buffer matches DefaultBuffer, so a test that reads in step with the test
// itself never blocks.
func collector() (Sender, <-chan string) {
	documents := make(chan string, event.DefaultBuffer)
	return func(data []byte) error {
		documents <- string(data)
		return nil
	}, documents
}

// receive requires exactly one document on documents and returns it.
func receive(t *testing.T, documents <-chan string) string {
	t.Helper()

	select {
	case got := <-documents:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no notification arrived within 5s")
		return ""
	}
}

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		stream string
		tag    string
	}{
		{name: "no stream selects the default", body: `<create-subscription/>`, stream: StreamName},
		{name: "explicit stream", body: `<create-subscription><stream>sim-events</stream></create-subscription>`, stream: StreamName},
		{name: "namespace is ignored", body: `<create-subscription xmlns="` + NotificationNamespace + `"><stream>sim-events</stream></create-subscription>`, stream: StreamName},
		{name: "unknown stream", body: `<create-subscription><stream>NETCONF</stream></create-subscription>`, tag: ops.TagInvalidValue},
		{name: "empty stream", body: `<create-subscription><stream> </stream></create-subscription>`, tag: ops.TagInvalidValue},
		{name: "filter", body: `<create-subscription><filter type="subtree"/></create-subscription>`, tag: ops.TagOperationNotSupported},
		{name: "startTime", body: `<create-subscription><startTime>2024-01-02T03:04:05Z</startTime></create-subscription>`, tag: ops.TagOperationNotSupported},
		{name: "stopTime", body: `<create-subscription><stopTime>2024-01-02T03:04:05Z</stopTime></create-subscription>`, tag: ops.TagOperationNotSupported},
		{name: "unknown element", body: `<create-subscription><interval>5</interval></create-subscription>`, tag: ops.TagUnknownElement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, opErr := ParseRequest(parseOperation(t, tt.body))

			if tt.tag != "" {
				require.NotNil(t, opErr)
				assert.Equal(t, tt.tag, opErr.Tag)
				return
			}
			require.Nil(t, opErr)
			assert.Equal(t, tt.stream, request.Stream)
		})
	}
}

func TestRenderNotification(t *testing.T) {
	when := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	data, err := Render(event.Event{
		Type:      event.TypeAlarmRaised,
		Resource:  "radio0",
		Severity:  "major",
		Message:   "radio link down",
		Timestamp: when,
	})

	require.NoError(t, err)
	assert.Equal(t,
		`<notification xmlns="`+NotificationNamespace+`">`+
			`<eventTime>2024-01-02T03:04:05Z</eventTime>`+
			`<event xmlns="`+EventsNamespace+`">`+
			`<type>AlarmRaised</type>`+
			`<resource>radio0</resource>`+
			`<severity>major</severity>`+
			`<message>radio link down</message>`+
			`</event></notification>`, string(data))
}

func TestRenderOmitsEmptyFieldsAndNormalizesTime(t *testing.T) {
	// A ConfigChanged event carries no severity, and its timestamp is rendered
	// in UTC with RFC 3339 precision.
	when := time.Date(2024, 6, 1, 12, 0, 1, 500_000_000, time.FixedZone("MSK", 3*60*60))

	data, err := Render(event.Event{Type: event.TypeConfigChanged, Resource: "device", Timestamp: when})

	require.NoError(t, err)
	assert.Equal(t,
		`<notification xmlns="`+NotificationNamespace+`">`+
			`<eventTime>2024-06-01T09:00:01.5Z</eventTime>`+
			`<event xmlns="`+EventsNamespace+`">`+
			`<type>ConfigChanged</type><resource>device</resource>`+
			`</event></notification>`, string(data))
}

func TestRenderStructuredAlarmAndTransitionFields(t *testing.T) {
	when := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	alarm, err := Render(event.Event{
		Type:      event.TypeAlarmRaised,
		Resource:  "radio0",
		Severity:  "critical",
		Message:   "radio link down",
		Domain:    event.DomainRadio,
		Alarm:     event.AlarmRadioLinkDown,
		Timestamp: when,
	})
	require.NoError(t, err)
	assert.Contains(t, string(alarm), `<alarm>radioLinkDown</alarm>`)
	assert.NotContains(t, string(alarm), `<from>`)
	assert.NotContains(t, string(alarm), `<to>`)

	transition, err := Render(event.Event{
		Type:      event.TypeStateTransition,
		Resource:  "ptp/clock",
		Domain:    event.DomainSync,
		From:      "locked",
		To:        "holdover-in-spec",
		Timestamp: when,
	})
	require.NoError(t, err)
	assert.Contains(t, string(transition), `<from>locked</from>`)
	assert.Contains(t, string(transition), `<to>holdover-in-spec</to>`)
	assert.NotContains(t, string(transition), `<alarm>`)
}

func TestSubscriptionDeliversQueuedNotifications(t *testing.T) {
	sender, documents := collector()
	sub := newSubscription(1, StreamName, sender)
	defer sub.stop()

	sub.deliver([]byte("first"))
	sub.deliver([]byte("second"))

	assert.Equal(t, "first", receive(t, documents))
	assert.Equal(t, "second", receive(t, documents))
}

func TestSubscriptionDropsWhenTheSessionIsNotReading(t *testing.T) {
	logs := captureLogs(t)

	block := make(chan struct{})
	defer close(block)
	entered := make(chan struct{})
	var once sync.Once
	sender := func([]byte) error {
		once.Do(func() { close(entered) })
		<-block
		return nil
	}
	sub := newSubscription(1, StreamName, sender)
	defer sub.stop()

	// The writer takes the first notification and blocks inside the sender, so
	// the buffer behind it can be filled exactly.
	sub.deliver([]byte("first"))
	<-entered
	for i := 0; i < event.DefaultBuffer; i++ {
		sub.deliver([]byte("queued"))
	}

	sub.deliver([]byte("dropped"))

	assert.Contains(t, logs.String(), "notification dropped")
}

func TestDispatcherDeliversToEverySubscription(t *testing.T) {
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	dispatcher := NewDispatcher(bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	firstSender, first := collector()
	secondSender, second := collector()
	require.NoError(t, dispatcher.Subscribe(1, StreamName, firstSender))
	require.NoError(t, dispatcher.Subscribe(2, StreamName, secondSender))
	assert.Equal(t, 2, dispatcher.Subscribers())

	bus.Publish(event.Event{
		Type:      event.TypeStateTransition,
		Resource:  "ptp0",
		Message:   "master -> holdover",
		Timestamp: time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC),
	})

	want := `<notification xmlns="` + NotificationNamespace + `">` +
		`<eventTime>2024-03-04T05:06:07Z</eventTime>` +
		`<event xmlns="` + EventsNamespace + `">` +
		`<type>StateTransition</type><resource>ptp0</resource><message>master -&gt; holdover</message>` +
		`</event></notification>`
	assert.Equal(t, want, receive(t, first))
	assert.Equal(t, want, receive(t, second))
}

func TestDispatcherSubscribeRejectsUnknownStream(t *testing.T) {
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	dispatcher := NewDispatcher(bus)

	sender, documents := collector()
	err := dispatcher.Subscribe(1, "alarms", sender)

	require.ErrorIs(t, err, ErrUnknownStream)
	assert.Equal(t, 0, dispatcher.Subscribers())
	select {
	case got := <-documents:
		t.Fatalf("unexpected notification %q", got)
	default:
	}
}

func TestDispatcherReplacesTheSubscriptionOfASession(t *testing.T) {
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	dispatcher := NewDispatcher(bus)

	firstSender, first := collector()
	secondSender, second := collector()
	require.NoError(t, dispatcher.Subscribe(1, StreamName, firstSender))
	require.NoError(t, dispatcher.Subscribe(1, StreamName, secondSender))

	assert.Equal(t, 1, dispatcher.Subscribers())

	dispatcher.dispatch(event.Event{Type: event.TypeAlarmRaised, Timestamp: time.Unix(0, 0).UTC()})

	select {
	case got := <-first:
		t.Fatalf("the replaced subscription received %q", got)
	default:
	}
	assert.NotEmpty(t, receive(t, second))
}

func TestDispatcherUnsubscribeStopsDelivery(t *testing.T) {
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	dispatcher := NewDispatcher(bus)

	sender, documents := collector()
	require.NoError(t, dispatcher.Subscribe(1, StreamName, sender))
	dispatcher.Unsubscribe(1)
	dispatcher.Unsubscribe(1)

	assert.Equal(t, 0, dispatcher.Subscribers())

	dispatcher.dispatch(event.Event{Type: event.TypeAlarmRaised, Timestamp: time.Unix(0, 0).UTC()})
	select {
	case got := <-documents:
		t.Fatalf("an unsubscribed session received %q", got)
	default:
	}
}

func TestDispatcherCloseReleasesTheBusAndStopsDelivery(t *testing.T) {
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	dispatcher := NewDispatcher(bus)

	sender, documents := collector()
	require.NoError(t, dispatcher.Subscribe(1, StreamName, sender))

	done := make(chan struct{})
	go func() {
		defer close(done)
		dispatcher.Run(context.Background())
	}()

	dispatcher.Close()
	dispatcher.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return once the dispatcher is closed")
	}

	bus.Publish(event.Event{Type: event.TypeAlarmRaised, Timestamp: time.Unix(0, 0).UTC()})
	select {
	case got := <-documents:
		t.Fatalf("a closed dispatcher delivered %q", got)
	default:
	}
	assert.ErrorIs(t, dispatcher.Subscribe(2, StreamName, func([]byte) error { return nil }), ErrClosed)
}

func TestDispatcherRunStopsOnContextCancel(t *testing.T) {
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	defer bus.Close()
	dispatcher := NewDispatcher(bus)
	defer dispatcher.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		dispatcher.Run(ctx)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return when the context is cancelled")
	}
}
