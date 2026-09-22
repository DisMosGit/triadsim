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
type Event struct {
	Type      EventType `json:"type"`
	Resource  string    `json:"resource"`
	Severity  string    `json:"severity,omitempty"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
