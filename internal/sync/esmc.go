package sync

import (
	"fmt"

	"github.com/DisMosGit/triadsim/internal/model"
)

// SSM codes of the ITU-T G.781 Option I quality levels, as carried in the
// 4-bit quality-level TLV of an ESMC PDU.
const (
	SSMCodePRC  uint8 = 0x2
	SSMCodeSSUA uint8 = 0x4
	SSMCodeSSUB uint8 = 0x8
	SSMCodeSEC  uint8 = 0xB
	SSMCodeDNU  uint8 = 0xF
)

// eSSM codes of the extended quality levels, from ITU-T G.8264 Amendment 2.
const (
	ExtendedSSMCodePRTC  uint8 = 0x20
	ExtendedSSMCodeEPRTC uint8 = 0x21
	ExtendedSSMCodeEEEC  uint8 = 0x22
)

// Quality-level ranks used by the source selection: a lower rank is better.
const (
	rankPRC  uint8 = 0
	rankSSUA uint8 = 1
	rankSSUB uint8 = 2
	rankSEC  uint8 = 3
)

// Message is one simplified ESMC PDU: the quality level an adjacent SyncE node
// advertises. The simulator never encodes it on the wire, so no framing or TLV
// layout is modelled.
type Message struct {
	// QL is the Option I quality level the message carries.
	QL model.QL
	// ExtendedQL is the optional eSSM code, and is empty when the sender does
	// not use extended codes.
	ExtendedQL string
}

// SSMCode returns the 4-bit SSM code of a quality level.
func SSMCode(ql model.QL) (uint8, bool) {
	switch ql {
	case model.QLPRC:
		return SSMCodePRC, true
	case model.QLSSUA:
		return SSMCodeSSUA, true
	case model.QLSSUB:
		return SSMCodeSSUB, true
	case model.QLSEC:
		return SSMCodeSEC, true
	case model.QLDNU:
		return SSMCodeDNU, true
	default:
		return 0, false
	}
}

// QLFromSSM is the inverse of SSMCode.
func QLFromSSM(code uint8) (model.QL, bool) {
	switch code {
	case SSMCodePRC:
		return model.QLPRC, true
	case SSMCodeSSUA:
		return model.QLSSUA, true
	case SSMCodeSSUB:
		return model.QLSSUB, true
	case SSMCodeSEC:
		return model.QLSEC, true
	case SSMCodeDNU:
		return model.QLDNU, true
	default:
		return "", false
	}
}

// ExtendedSSMCode returns the code of an extended quality level. The empty
// value means the sender does not use extended codes, and is reported as zero
// with ok true.
func ExtendedSSMCode(ql string) (uint8, bool) {
	switch ql {
	case "":
		return 0, true
	case model.ExtendedQLPRTC:
		return ExtendedSSMCodePRTC, true
	case model.ExtendedQLPRTCEnhanced:
		return ExtendedSSMCodeEPRTC, true
	case model.ExtendedQLEECEnhanced:
		return ExtendedSSMCodeEEEC, true
	default:
		return 0, false
	}
}

// Rank returns the selection rank of a quality level: a lower rank is better.
// QL-DNU is never selectable and is reported with ok false.
func Rank(ql model.QL) (uint8, bool) {
	switch ql {
	case model.QLPRC:
		return rankPRC, true
	case model.QLSSUA:
		return rankSSUA, true
	case model.QLSSUB:
		return rankSSUB, true
	case model.QLSEC:
		return rankSEC, true
	default:
		return 0, false
	}
}

// Validate reports whether the message carries a known quality level and a
// known, or empty, extended quality level.
func (m Message) Validate() error {
	if err := m.QL.Validate(); err != nil {
		return fmt.Errorf("ql: %w", err)
	}
	if _, ok := ExtendedSSMCode(m.ExtendedQL); !ok {
		return fmt.Errorf("unknown extended quality level %q", m.ExtendedQL)
	}
	return nil
}
