// Package event provides the simulator's in-process event bus.
//
// Domains never import each other: they publish typed events on the bus and
// react to the events they subscribe to. The bus is the only cross-domain
// channel, which keeps radio, l2 and sync independent.
package event

import "time"

// EventType identifies the kind of an Event.
type EventType string

// Event types published by the simulator.
const (
	// TypeAlarmRaised announces a new alarm, for example radioLinkDown.
	TypeAlarmRaised EventType = "AlarmRaised"
	// TypeAlarmCleared announces that an alarm condition ended.
	TypeAlarmCleared EventType = "AlarmCleared"
	// TypeConfigChanged announces that a commit changed the running
	// configuration.
	TypeConfigChanged EventType = "ConfigChanged"
	// TypeStateTransition announces a domain state-machine transition, for
	// example PTP master to holdover.
	TypeStateTransition EventType = "StateTransition"
)

// Event is a single notification on the bus.
//
// Resource names the affected object (for example "radio0"), Severity carries
// the alarm severity for alarm events, and Timestamp is set by the publisher —
// normally from its injected clock.Clock.
//
// The optional structured fields keep consumers — the SNMP trap sender, the
// metrics collector — from parsing Message text: Domain is the publishing
// domain that labels an alarm's family, Alarm names the alarm condition, and
// From/To are the states of a StateTransition. An alarm keeps its Alarm name on
// both the raised and the cleared event; Type says which of the two it is.
type Event struct {
	Type      EventType `json:"type"`
	Resource  string    `json:"resource"`
	Severity  string    `json:"severity,omitempty"`
	Message   string    `json:"message,omitempty"`
	Domain    string    `json:"domain,omitempty"`
	Alarm     string    `json:"alarm,omitempty"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Alarm family values carried in Event.Domain. They are the type label of the
// simulator_alarms_total metric.
const (
	// DomainRadio is the radio-link (RRL) domain.
	DomainRadio = "radio"
	// DomainL2 is the L2 switching domain.
	DomainL2 = "l2"
	// DomainSync is the synchronization domain.
	DomainSync = "sync"
)

// Alarm names carried in Event.Alarm.
const (
	// AlarmRadioLinkDown is the radio-link loss-of-signal alarm.
	AlarmRadioLinkDown = "radioLinkDown"
	// AlarmRadioLinkDegraded is the radio-link degradation alarm.
	AlarmRadioLinkDegraded = "radioLinkDegraded"
	// AlarmL2Storm is the broadcast-storm alarm.
	AlarmL2Storm = "l2Storm"
	// AlarmSyncHoldover is the synchronization holdover alarm.
	AlarmSyncHoldover = "syncHoldover"
)
