package netconf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapabilitiesOnlyAdvertiseImplementedFeatures(t *testing.T) {
	capabilities := Capabilities()

	assert.Equal(t, []string{
		CapabilityBase11,
		CapabilityBase10,
		CapabilityCandidate,
		CapabilityWritableRunning,
	}, capabilities)

	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		_, duplicate := seen[capability]
		assert.False(t, duplicate, "duplicate capability %s", capability)
		seen[capability] = struct{}{}
	}
}

func TestServerHelloRoundTrip(t *testing.T) {
	data, err := marshalHello(serverHello(42))
	require.NoError(t, err)

	text := string(data)
	assert.True(t, strings.HasPrefix(text, `<hello xmlns="`+BaseNamespace+`">`), text)
	assert.Contains(t, text, "<session-id>42</session-id>")
	for _, capability := range Capabilities() {
		assert.Contains(t, text, "<capability>"+capability+"</capability>")
	}

	message, err := parseHello(data)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), message.SessionID)
	assert.ElementsMatch(t, Capabilities(), message.Capabilities)
	assert.True(t, message.chunked())
}

func TestParseHelloAcceptsChunkedAndEOMClients(t *testing.T) {
	tests := []struct {
		name    string
		hello   string
		chunked bool
	}{
		{
			name:    "base 1.1 negotiates chunked framing",
			hello:   `<hello xmlns="` + BaseNamespace + `"><capabilities><capability>` + CapabilityBase11 + `</capability><capability>` + CapabilityBase10 + `</capability></capabilities><session-id>1</session-id></hello>`,
			chunked: true,
		},
		{
			name:    "base 1.0 keeps end-of-message framing",
			hello:   `<hello><capabilities><capability>` + CapabilityBase10 + `</capability></capabilities><session-id>2</session-id></hello>`,
			chunked: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message, err := parseHello([]byte(tt.hello))

			require.NoError(t, err)
			assert.Equal(t, tt.chunked, message.chunked())
		})
	}
}

func TestParseHelloRejectsInvalidHellos(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "missing base capability",
			data: `<hello><capabilities><capability>` + CapabilityCandidate + `</capability></capabilities><session-id>1</session-id></hello>`,
		},
		{
			name: "wrong root element",
			data: `<rpc message-id="1"><get-config/></rpc>`,
		},
		{
			name: "malformed xml",
			data: `<hello><capabilities>`,
		},
		{
			name: "not xml at all",
			data: `hello`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseHello([]byte(tt.data))

			assert.Error(t, err)
		})
	}
}

func TestSessionIDsAreMonotonic(t *testing.T) {
	r, st := newTestStore(t)
	srv := New(r, st, nil, Options{Addr: "127.0.0.1:0"})

	first := srv.nextSessionID()
	second := srv.nextSessionID()

	require.NotZero(t, first)
	assert.Equal(t, first+1, second)
}
