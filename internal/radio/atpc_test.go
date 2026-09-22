package radio

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DisMosGit/triadsim/internal/model"
)

func TestATPCStep(t *testing.T) {
	base := model.ATPC{
		Enabled:      true,
		TargetRSL:    -45,
		MinPower:     -10,
		MaxPower:     30,
		Range:        20,
		CurrentPower: 18.5,
	}

	tests := []struct {
		name     string
		mutate   func(*model.ATPC)
		measured float64
		want     float64
	}{
		{name: "steps up by the limit", measured: -50, want: 19.5},
		{name: "steps down by the limit", measured: -40, want: 17.5},
		{name: "corrects by less than the limit", measured: -45.5, want: 19.0},
		{name: "at target", measured: -45, want: 18.5},
		{
			name:     "clamps at max-power",
			mutate:   func(a *model.ATPC) { a.CurrentPower = a.MaxPower },
			measured: -80,
			want:     30,
		},
		{
			name:     "clamps at min-power",
			mutate:   func(a *model.ATPC) { a.CurrentPower = a.MinPower },
			measured: 0,
			want:     -10,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			atpc := base
			if test.mutate != nil {
				test.mutate(&atpc)
			}
			assert.InDelta(t, test.want, atpcStep(atpc, test.measured, DefaultATPCStepDB), 0.001)
		})
	}
}
