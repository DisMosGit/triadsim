package gnmi

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DisMosGit/triadsim/internal/event"
)

func TestAffectedPaths(t *testing.T) {
	tests := []struct {
		name string
		when event.Event
		want []string
	}{
		{
			name: "configuration change concerns every subscription",
			when: event.Event{Type: event.TypeConfigChanged, Resource: "device"},
			want: []string{""},
		},
		{
			name: "radio alarm maps onto the link's interface",
			when: event.Event{
				Type:     event.TypeAlarmRaised,
				Resource: "radio0",
				Alarm:    event.AlarmRadioLinkDown,
			},
			want: []string{"interfaces/interface[name=radio0]/radio-link"},
		},
		{
			name: "radio clear maps onto the same interface",
			when: event.Event{
				Type:     event.TypeAlarmCleared,
				Resource: "radio0",
				Alarm:    event.AlarmRadioLinkDown,
			},
			want: []string{"interfaces/interface[name=radio0]/radio-link"},
		},
		{
			name: "ptp transition",
			when: event.Event{Type: event.TypeStateTransition, Resource: "ptp/clock"},
			want: []string{"ptp/clock"},
		},
		{
			name: "stp transition",
			when: event.Event{Type: event.TypeStateTransition, Resource: "l2/stp/eth0"},
			want: []string{"stp/state/ports/port[name=eth0]"},
		},
		{
			name: "broadcast storm",
			when: event.Event{Type: event.TypeAlarmRaised, Resource: "l2/storm/eth1"},
			want: []string{"interfaces/interface[name=eth1]"},
		},
		{
			name: "an unknown resource is not mapped",
			when: event.Event{Type: event.TypeAlarmRaised, Resource: "l2/something/eth0"},
			want: nil,
		},
		{
			name: "an empty resource is not mapped",
			when: event.Event{Type: event.TypeAlarmRaised},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, affectedPaths(test.when))
		})
	}
}

func TestMatchesSubscription(t *testing.T) {
	tests := []struct {
		name         string
		subscription string
		affected     string
		want         bool
	}{
		{name: "root subscription", subscription: "", affected: "ptp/clock", want: true},
		{name: "exact", subscription: "ptp/clock", affected: "ptp/clock", want: true},
		{name: "child of subscription", subscription: "ptp", affected: "ptp/clock", want: true},
		{name: "parent of subscription", subscription: "ptp/clock", affected: "ptp", want: true},
		{name: "unrelated", subscription: "ptp/clock", affected: "vlans", want: false},
		{name: "a configuration change reaches everybody", subscription: "vlans", affected: "", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, matchesSubscription(test.subscription, test.affected))
		})
	}
}

func TestNotificationTarget(t *testing.T) {
	assert.Equal(t, "ptp/clock", notificationTarget("ptp/clock", "ptp/clock/state"))
	assert.Equal(t, "ptp/clock", notificationTarget("ptp/clock", "ptp"))
	assert.Equal(t, "ptp/clock/state", notificationTarget("", "ptp/clock/state"))
}
