package radio

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DisMosGit/triadsim/internal/model"
)

// defaultThresholds is the alarm and clear levels of the domain defaults.
func defaultThresholds() thresholds {
	return thresholds{
		rssiAlarm: DefaultRSSIAlarmThreshold,
		rssiClear: DefaultRSSIClearThreshold,
		fadeMin:   DefaultFadeMarginMinDB,
		fadeClear: DefaultFadeMarginClearDB,
	}
}

func TestNextLinkState(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		rssi       float64
		fadeMargin float64
		want       string
	}{
		{name: "healthy stays up", current: model.RadioLinkStateUp, rssi: -70, fadeMargin: 12, want: model.RadioLinkStateUp},
		{name: "unknown healthy is up", current: "", rssi: -70, fadeMargin: 12, want: model.RadioLinkStateUp},

		{name: "quality drop degrades", current: model.RadioLinkStateUp, rssi: -78, fadeMargin: 5, want: model.RadioLinkStateDegraded},
		{name: "unknown weak link degrades", current: "", rssi: -78, fadeMargin: 5, want: model.RadioLinkStateDegraded},
		{name: "degraded holds below the clear margin", current: model.RadioLinkStateDegraded, rssi: -78, fadeMargin: 7, want: model.RadioLinkStateDegraded},
		{name: "degraded recovers above the clear margin", current: model.RadioLinkStateDegraded, rssi: -70, fadeMargin: 8, want: model.RadioLinkStateUp},

		{name: "low level goes down", current: model.RadioLinkStateUp, rssi: -86, fadeMargin: -3, want: model.RadioLinkStateDown},
		{name: "down when degraded too", current: model.RadioLinkStateDegraded, rssi: -86, fadeMargin: -3, want: model.RadioLinkStateDown},
		{name: "down holds between the thresholds", current: model.RadioLinkStateDown, rssi: -84, fadeMargin: -4, want: model.RadioLinkStateDown},
		{name: "down recovers past the clear threshold", current: model.RadioLinkStateDown, rssi: -81.9, fadeMargin: 2, want: model.RadioLinkStateDegraded},
		{name: "down recovers to up", current: model.RadioLinkStateDown, rssi: -60, fadeMargin: 12, want: model.RadioLinkStateUp},

		// With no usable profile the fade margin is infinite, so only the
		// received level drives the state.
		{name: "no profile still detects loss", current: model.RadioLinkStateUp, rssi: -90, fadeMargin: math.Inf(1), want: model.RadioLinkStateDown},
		{name: "no profile never degrades", current: model.RadioLinkStateUp, rssi: -70, fadeMargin: math.Inf(1), want: model.RadioLinkStateUp},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, nextLinkState(test.current, test.rssi, test.fadeMargin, defaultThresholds()))
		})
	}
}
