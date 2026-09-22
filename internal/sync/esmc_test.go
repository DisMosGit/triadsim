package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/model"
)

func TestSSMCodeRoundTrip(t *testing.T) {
	tests := []struct {
		ql   model.QL
		code uint8
	}{
		{ql: model.QLPRC, code: SSMCodePRC},
		{ql: model.QLSSUA, code: SSMCodeSSUA},
		{ql: model.QLSSUB, code: SSMCodeSSUB},
		{ql: model.QLSEC, code: SSMCodeSEC},
		{ql: model.QLDNU, code: SSMCodeDNU},
	}

	for _, tt := range tests {
		t.Run(string(tt.ql), func(t *testing.T) {
			code, ok := SSMCode(tt.ql)
			require.True(t, ok)
			assert.Equal(t, tt.code, code)

			ql, ok := QLFromSSM(code)
			require.True(t, ok)
			assert.Equal(t, tt.ql, ql)
		})
	}

	_, ok := SSMCode("QL-NOPE")
	assert.False(t, ok)

	_, ok = QLFromSSM(0x1)
	assert.False(t, ok)
}

func TestExtendedSSMCode(t *testing.T) {
	tests := []struct {
		name string
		ql   string
		code uint8
		ok   bool
	}{
		{name: "no extended codes", ql: "", code: 0, ok: true},
		{name: "PRTC", ql: model.ExtendedQLPRTC, code: ExtendedSSMCodePRTC, ok: true},
		{name: "ePRTC", ql: model.ExtendedQLPRTCEnhanced, code: ExtendedSSMCodeEPRTC, ok: true},
		{name: "eEEC", ql: model.ExtendedQLEECEnhanced, code: ExtendedSSMCodeEEEC, ok: true},
		{name: "unknown", ql: "QL-NOPE", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := ExtendedSSMCode(tt.ql)

			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.code, code)
		})
	}
}

func TestRank(t *testing.T) {
	tests := []struct {
		ql   model.QL
		rank uint8
		ok   bool
	}{
		{ql: model.QLPRC, rank: rankPRC, ok: true},
		{ql: model.QLSSUA, rank: rankSSUA, ok: true},
		{ql: model.QLSSUB, rank: rankSSUB, ok: true},
		{ql: model.QLSEC, rank: rankSEC, ok: true},
		{ql: model.QLDNU, ok: false},
		{ql: "", ok: false},
		{ql: "QL-NOPE", ok: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.ql), func(t *testing.T) {
			rank, ok := Rank(tt.ql)

			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.rank, rank)
		})
	}
}

func TestMessageValidate(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		wantErr bool
	}{
		{name: "option I only", message: Message{QL: model.QLPRC}},
		{name: "with eSSM", message: Message{QL: model.QLPRC, ExtendedQL: model.ExtendedQLPRTCEnhanced}},
		{name: "unknown ql", message: Message{QL: "QL-NOPE"}, wantErr: true},
		{name: "empty ql", message: Message{}, wantErr: true},
		{name: "unknown extended ql", message: Message{QL: model.QLSEC, ExtendedQL: "QL-NOPE"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.message.Validate()

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
