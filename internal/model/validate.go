// Validation helpers shared by the model types.
//
// The rules live in the Validate methods; these helpers only keep the numeric
// and address checks consistent across files.
package model

import (
	"math"
	"net"
)

// finite reports whether v is neither NaN nor an infinity.
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// inRange reports whether v is finite and lies within [min, max].
func inRange(v, min, max float64) bool {
	return finite(v) && v >= min && v <= max
}

// validMAC reports whether s parses as a 48-bit (6-byte) MAC address.
func validMAC(s string) bool {
	hw, err := net.ParseMAC(s)
	return err == nil && len(hw) == 6
}
