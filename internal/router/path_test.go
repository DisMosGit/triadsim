package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		path string
		want []Segment
	}{
		{
			name: "scalar",
			path: "system-info/device-id",
			want: []Segment{{Name: "system-info"}, {Name: "device-id"}},
		},
		{
			name: "list key",
			path: "interfaces/interface[name=radio0]/radio-link/tx-power",
			want: []Segment{
				{Name: "interfaces"},
				{Name: "interface", Key: "name", Value: "radio0"},
				{Name: "radio-link"},
				{Name: "tx-power"},
			},
		},
		{
			name: "numeric key",
			path: "interfaces/interface[name=radio0]/radio-link/modulation-profile[id=5]/capacity",
			want: []Segment{
				{Name: "interfaces"},
				{Name: "interface", Key: "name", Value: "radio0"},
				{Name: "radio-link"},
				{Name: "modulation-profile", Key: "id", Value: "5"},
				{Name: "capacity"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := Parse(tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, parsed.Segments)
			assert.Equal(t, tt.path, parsed.String())
			assert.False(t, parsed.IsZero())
		})
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "empty", path: ""},
		{name: "leading slash", path: "/a"},
		{name: "trailing slash", path: "a/"},
		{name: "double slash", path: "a//b"},
		{name: "unterminated predicate", path: "a[b=c"},
		{name: "empty predicate", path: "a[]"},
		{name: "missing value", path: "a[b=]"},
		{name: "missing key", path: "a[=c]"},
		{name: "missing equals", path: "a[bc]"},
		{name: "text after predicate", path: "a[b=c]d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.path)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidPath)
		})
	}
}

func TestCompareOIDNumericOrder(t *testing.T) {
	assert.Equal(t, -1, CompareOID("1.3.6.1.2.1.2.2.1.2.2", "1.3.6.1.2.1.2.2.1.2.10"))
	assert.Equal(t, 1, CompareOID("1.3.6.1.2.1.2.2.1.2.10", "1.3.6.1.2.1.2.2.1.2.2"))
	assert.Equal(t, 0, CompareOID("1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.1.0"))
	assert.Equal(t, -1, CompareOID("1.3.6.1.2.1.1", "1.3.6.1.2.1.1.1"))
	assert.Equal(t, 1, CompareOID("1.3.6.1.2.1.2", "1.3.6.1.2.1.1"))
}
