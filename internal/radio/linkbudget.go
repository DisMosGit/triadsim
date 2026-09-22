package radio

import (
	"math"

	"github.com/DisMosGit/triadsim/internal/model"
)

// Loss terms of the simulated free-space link, in dB. A real link budget also
// accounts for atmospheric absorption and miscellaneous losses (pointing,
// radome); the simulator keeps them as constants.
const (
	atmLossDB  = 0.5
	miscLossDB = 0.5
)

// freeSpaceLossDB returns the ITU-R P.530 free-space path loss of a link, in
// dB:
//
//	L_FS = 92.45 + 20*log10(f_GHz) + 20*log10(d_km)
func freeSpaceLossDB(frequencyGHz, lengthKm float64) float64 {
	return 92.45 + 20*math.Log10(frequencyGHz) + 20*math.Log10(lengthKm)
}

// effectiveTxPower returns the transmit power a budget starts from: the
// ATPC-adjusted current power while ATPC is enabled, the configured tx-power
// otherwise.
func effectiveTxPower(link model.RadioLink) float64 {
	if link.ATPC.Enabled {
		return link.ATPC.CurrentPower
	}
	return link.TxPower
}

// receivedLevel returns the clear-sky received signal level of a link, before
// any injected fade:
//
//	RSL = P_TX + G_TX + G_RX - L_FS - L_feed - L_atm - L_misc
func receivedLevel(link model.RadioLink, txPower float64) float64 {
	budget := link.LinkBudget
	return txPower + budget.TxAntennaGain + budget.RxAntennaGain -
		freeSpaceLossDB(budget.Frequency, budget.LinkLength) - budget.FeedLoss - atmLossDB - miscLossDB
}

// applyFade returns the received level after an injected fade, bounded to the
// range the model accepts for rssi and calculated-rsl: a domain state write
// must never make a later commit fail.
func applyFade(level, fadeDB float64) float64 {
	return clamp(level-fadeDB, model.RSSIMinDBM, model.RSSIMaxDBM)
}
