package radio

import (
	"math"

	"github.com/DisMosGit/triadsim/internal/model"
)

// clamp bounds value to [min, max].
func clamp(value, min, max float64) float64 {
	return math.Min(math.Max(value, min), max)
}

// atpcStep returns the transmit power after one ATPC control step towards the
// configured target received level:
//
//	delta = clamp(target - measured, -step, +step)
//	next  = clamp(current + delta, min-power, max-power)
//
// The step is what keeps the simulated control loop gradual: the whole ATPC
// range is walked in steps of stepDB, not in one jump.
func atpcStep(atpc model.ATPC, measuredRSL, stepDB float64) float64 {
	delta := clamp(atpc.TargetRSL-measuredRSL, -stepDB, stepDB)
	return clamp(atpc.CurrentPower+delta, atpc.MinPower, atpc.MaxPower)
}
