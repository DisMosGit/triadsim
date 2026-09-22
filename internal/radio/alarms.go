package radio

import (
	"fmt"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/model"
)

// thresholds are the raise and clear levels of the link-state machine.
type thresholds struct {
	rssiAlarm float64
	rssiClear float64
	fadeMin   float64
	fadeClear float64
}

// nextLinkState returns the link state one measurement implies. The raise and
// the clear levels differ, so a level sitting on a threshold does not flap the
// state.
func nextLinkState(current string, rssi, fadeMargin float64, t thresholds) string {
	if linkDown(current, rssi, t) {
		return model.RadioLinkStateDown
	}
	if linkDegraded(current, fadeMargin, t) {
		return model.RadioLinkStateDegraded
	}
	return model.RadioLinkStateUp
}

// linkDown reports the loss-of-signal condition. A link that is already down
// stays down until the level recovers past the clear threshold.
func linkDown(current string, rssi float64, t thresholds) bool {
	if current == model.RadioLinkStateDown {
		return rssi < t.rssiClear
	}
	return rssi < t.rssiAlarm
}

// linkDegraded reports the fade-margin condition with the same hysteresis.
func linkDegraded(current string, fadeMargin float64, t thresholds) bool {
	if current == model.RadioLinkStateDegraded {
		return fadeMargin < t.fadeClear
	}
	return fadeMargin < t.fadeMin
}

// report publishes the alarm transition of one link-state change: the alarm the
// link leaves is cleared first, then the alarm it enters is raised. A state
// change that only involves the up state publishes a single clear.
func (m *Manager) report(name, current, next string, rssi, fadeMargin float64) {
	cleared := func(alarm, message string) {
		m.publish(event.Event{
			Type:     event.TypeAlarmCleared,
			Resource: name,
			Severity: "cleared",
			Domain:   event.DomainRadio,
			Alarm:    alarm,
			Message:  message,
		})
	}
	raised := func(alarm, severity, message string) {
		m.publish(event.Event{
			Type:     event.TypeAlarmRaised,
			Resource: name,
			Severity: severity,
			Domain:   event.DomainRadio,
			Alarm:    alarm,
			Message:  message,
		})
	}

	switch current {
	case model.RadioLinkStateDown:
		cleared(event.AlarmRadioLinkDown,
			fmt.Sprintf("radio link %s up: rssi %.1f dBm", name, rssi))
	case model.RadioLinkStateDegraded:
		cleared(event.AlarmRadioLinkDegraded,
			fmt.Sprintf("radio link %s recovered: fade margin %.1f dB", name, fadeMargin))
	}

	switch next {
	case model.RadioLinkStateDown:
		raised(event.AlarmRadioLinkDown, "critical",
			fmt.Sprintf("radio link %s down: rssi %.1f dBm", name, rssi))
	case model.RadioLinkStateDegraded:
		raised(event.AlarmRadioLinkDegraded, "major",
			fmt.Sprintf("radio link %s degraded: fade margin %.1f dB", name, fadeMargin))
	}
}
