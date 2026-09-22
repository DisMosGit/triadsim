package netconf

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// frame encodes messages with the production writer.
func frame(t *testing.T, mode framingMode, messages ...string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := newMessageWriter(&buffer, mode)
	for _, message := range messages {
		require.NoError(t, writer.WriteMessage([]byte(message)))
	}
	return buffer.Bytes()
}

// readFramed decodes every message from wire.
func readFramed(t *testing.T, mode framingMode, reader io.Reader) []string {
	t.Helper()

	decoder := newMessageReader(bufio.NewReader(reader), mode)
	var messages []string
	for {
		message, err := decoder.ReadMessage()
		if errors.Is(err, io.EOF) {
			return messages
		}
		require.NoError(t, err)
		messages = append(messages, string(message))
	}
}

func TestFramingRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		mode    framingMode
		message string
	}{
		{name: "end-of-message", mode: framingEOM, message: `<rpc message-id="1"><get-config/></rpc>`},
		{name: "chunked", mode: framingChunked, message: `<rpc message-id="1"><get-config/></rpc>`},
		{name: "chunked empty message", mode: framingChunked, message: ``},
		{name: "eom keeps the delimiter inside text", mode: framingEOM, message: `<data>no delimiter here</data>`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := frame(t, tt.mode, tt.message)

			assert.Equal(t, []string{tt.message}, readFramed(t, tt.mode, bytes.NewReader(wire)))
		})
	}
}

func TestEOMWireFormat(t *testing.T) {
	wire := frame(t, framingEOM, `<hello/>`)

	assert.Equal(t, `<hello/>`+endOfMessage, string(wire))
}

func TestChunkedWireFormatIsHexadecimal(t *testing.T) {
	message := strings.Repeat("x", 26)
	wire := frame(t, framingChunked, message)

	// 26 is 0x1a: RFC 6242 declares chunk-size as hexadecimal.
	assert.Equal(t, "\n#1a\n"+message+"\n##\n", string(wire))
}

func TestChunkedReaderAcceptsSeveralChunks(t *testing.T) {
	wire := "\n#3\n<rp\n#2\nc>\n##\n"

	assert.Equal(t, []string{"<rpc>"}, readFramed(t, framingChunked, strings.NewReader(wire)))
}

func TestChunkedReaderAcceptsLFOnlyBeforeTheFirstChunk(t *testing.T) {
	// Some peers write the leading LF once and then separate chunks with it.
	wire := "#3\n<rp\n#2\nc>\n##\n"

	assert.Equal(t, []string{"<rpc>"}, readFramed(t, framingChunked, strings.NewReader(wire)))
}

func TestFramingSurvivesSplitReads(t *testing.T) {
	wire := frame(t, framingChunked, `<rpc message-id="2"><commit/></rpc>`)

	reader := iotest.OneByteReader(bytes.NewReader(wire))
	assert.Equal(t,
		[]string{`<rpc message-id="2"><commit/></rpc>`},
		readFramed(t, framingChunked, reader))
}

func TestFramingReadsSeveralMessages(t *testing.T) {
	wire := frame(t, framingChunked, `<a/>`, `<b/>`)

	assert.Equal(t, []string{"<a/>", "<b/>"}, readFramed(t, framingChunked, bytes.NewReader(wire)))
}

func TestFramingErrors(t *testing.T) {
	tests := []struct {
		name string
		mode framingMode
		wire string
		want error
	}{
		{
			name: "eom without delimiter",
			mode: framingEOM,
			wire: `<rpc message-id="1"/>`,
			want: io.EOF,
		},
		{
			name: "chunk header is not a hash",
			mode: framingChunked,
			wire: `\n#4\n<rpc/>\n##\n`,
			want: errMalformedFrame,
		},
		{
			name: "chunk size is not hexadecimal",
			mode: framingChunked,
			wire: "\n#zz\n<rpc/>\n##\n",
			want: errMalformedFrame,
		},
		{
			name: "chunk data is truncated",
			mode: framingChunked,
			wire: "\n#10\nshort",
			want: io.ErrUnexpectedEOF,
		},
		{
			name: "chunk size exceeds the message cap",
			mode: framingChunked,
			wire: "\n#ffffffff\n",
			want: errMessageTooBig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newMessageReader(bufio.NewReader(strings.NewReader(tt.wire)), tt.mode)

			_, err := reader.ReadMessage()

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

func TestDetectFraming(t *testing.T) {
	tests := []struct {
		name string
		wire string
		want framingMode
	}{
		{name: "xml is end-of-message", wire: `<hello/>`, want: framingEOM},
		{name: "leading newline then xml is end-of-message", wire: "\n<hello/>", want: framingEOM},
		{name: "hash is chunked", wire: "#d\n<hello/>\n##\n", want: framingChunked},
		{name: "leading newline then hash is chunked", wire: "\n#d\n<hello/>\n##\n", want: framingChunked},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := detectFraming(bufio.NewReader(strings.NewReader(tt.wire)))

			require.NoError(t, err)
			assert.Equal(t, tt.want, mode)
		})
	}
}

func TestFramingModeString(t *testing.T) {
	assert.Equal(t, "eom", framingEOM.String())
	assert.Equal(t, "chunked", framingChunked.String())
}
