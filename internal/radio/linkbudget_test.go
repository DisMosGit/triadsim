package radio

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/DisMosGit/triadsim/internal/model"
)

func TestFreeSpaceLoss(t *testing.T) {
	// 18 GHz over 12.5 km is the seeded link of model.DefaultDevice.
	assert.InDelta(t, 139.4937, freeSpaceLossDB(18, 12.5), 0.001)
	// Doubling the distance adds 20*log10(2) = 6.02 dB.
	assert.InDelta(t, freeSpaceLossDB(18, 25)-freeSpaceLossDB(18, 12.5), 6.0206, 0.001)
}

func TestReceivedLevelUsesTheEffectiveTransmitPower(t *testing.T) {
	device := model.DefaultDevice()
	link := *device.Interfaces[0].RadioLink

	// ATPC enabled: the budget starts from the ATPC-adjusted power.
	assert.Equal(t, link.ATPC.CurrentPower, effectiveTxPower(link))
	assert.InDelta(t, -47.9937, receivedLevel(link, effectiveTxPower(link)), 0.01)

	// ATPC disabled: it starts from the configured tx-power.
	link.ATPC.Enabled = false
	link.TxPower = 20
	assert.Equal(t, 20.0, effectiveTxPower(link))
	assert.InDelta(t, -46.4937, receivedLevel(link, effectiveTxPower(link)), 0.01)
}

func TestApplyFadeClampsToTheModelRange(t *testing.T) {
	assert.InDelta(t, -50, applyFade(-40, 10), 0.001)
	assert.Equal(t, model.RSSIMinDBM, applyFade(-40, 100))
	assert.Equal(t, model.RSSIMaxDBM, applyFade(-21, -5))
}
