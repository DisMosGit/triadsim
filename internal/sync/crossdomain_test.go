package sync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/event"
)

// A radio-link failure takes the clock's reference away: the alarm the radio
// domain publishes drives the PTP clock into holdover, and the cleared alarm
// brings it back. The domains never import each other; the alarm name is the
// whole contract.
func TestRadioAlarmsDriveTheClock(t *testing.T) {
	f := newFixture(t)

	require.NoError(t, f.manager.handleDomainEvent(t.Context(), event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "radio0",
		Severity: "critical",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	}))
	assert.Equal(t, string(StateHoldoverInSpec), f.state(t))

	require.NoError(t, f.manager.handleDomainEvent(t.Context(), event.Event{
		Type:     event.TypeAlarmCleared,
		Resource: "radio0",
		Severity: "cleared",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	}))
	assert.Equal(t, string(StateLocked), f.state(t))
}

// Events of the other domains, and the radio degradation alarm, do not move the
// clock.
func TestOtherDomainEventsAreIgnored(t *testing.T) {
	f := newFixture(t)

	events := []event.Event{
		{Type: event.TypeAlarmRaised, Domain: event.DomainL2, Alarm: event.AlarmL2Storm},
		{Type: event.TypeStateTransition, Domain: event.DomainL2, From: "discarding", To: "forwarding"},
		{Type: event.TypeConfigChanged, Resource: "device"},
		{Type: event.TypeAlarmRaised, Domain: event.DomainRadio, Alarm: event.AlarmRadioLinkDegraded},
	}
	for _, e := range events {
		require.NoError(t, f.manager.handleDomainEvent(t.Context(), e))
	}
	assert.Equal(t, string(StateLocked), f.state(t))
}

// The manager takes its bus subscription when it is built, so Run sees an alarm
// that was published before it started.
func TestRunReactsToARadioAlarm(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go f.manager.Run(ctx)

	f.bus.Publish(event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "radio0",
		Severity: "critical",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	})

	require.Eventually(t, func() bool {
		return f.state(t) == string(StateHoldoverInSpec)
	}, 5*time.Second, 5*time.Millisecond)
}
