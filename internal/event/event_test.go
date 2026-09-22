package event

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventTypeValues(t *testing.T) {
	tests := []struct {
		name string
		typ  EventType
		want string
	}{
		{name: "alarm raised", typ: TypeAlarmRaised, want: "AlarmRaised"},
		{name: "alarm cleared", typ: TypeAlarmCleared, want: "AlarmCleared"},
		{name: "config changed", typ: TypeConfigChanged, want: "ConfigChanged"},
		{name: "state transition", typ: TypeStateTransition, want: "StateTransition"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.typ))
		})
	}
}

func TestEventJSON(t *testing.T) {
	ts := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	e := Event{
		Type:      TypeAlarmRaised,
		Resource:  "radio0",
		Severity:  "critical",
		Message:   "link down",
		Timestamp: ts,
	}

	data, err := json.Marshal(e)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type": "AlarmRaised",
		"resource": "radio0",
		"severity": "critical",
		"message": "link down",
		"timestamp": "2026-01-02T03:04:05Z"
	}`, string(data))

	var got Event
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, e, got)
}

func TestEventJSONOmitsEmptySeverityAndMessage(t *testing.T) {
	data, err := json.Marshal(Event{Type: TypeStateTransition, Resource: "ptp"})
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	assert.NotContains(t, fields, "severity")
	assert.NotContains(t, fields, "message")
}
