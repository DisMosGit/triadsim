package netconf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/netconf/ops"
	"github.com/DisMosGit/triadsim/internal/store"
)

func TestParseRPC(t *testing.T) {
	valid := `<rpc message-id="101" xmlns="` + BaseNamespace + `"><get-config><source><running/></source></get-config></rpc>`

	rpc, parseErr := parseRPC([]byte(valid))
	require.Nil(t, parseErr)
	assert.Equal(t, "101", rpc.MessageID)
	assert.Contains(t, string(rpc.Body), "<get-config>")

	tests := []struct {
		name string
		data string
		tag  string
	}{
		{
			name: "wrong root element",
			data: `<get-config><source><running/></source></get-config>`,
			tag:  ops.TagMalformedMessage,
		},
		{
			name: "missing message-id",
			data: `<rpc><close-session/></rpc>`,
			tag:  ops.TagMissingAttribute,
		},
		{
			name: "blank message-id",
			data: `<rpc message-id=" "><close-session/></rpc>`,
			tag:  ops.TagMissingAttribute,
		},
		{
			name: "malformed xml",
			data: `<rpc message-id="1"><close-session>`,
			tag:  ops.TagMalformedMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, parseErr := parseRPC([]byte(tt.data))

			require.NotNil(t, parseErr)
			assert.Equal(t, tt.tag, parseErr.Tag)
			assert.Equal(t, ops.TypeRPC, parseErr.Type)
		})
	}
}

func TestReplyMarshalling(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		data, err := okReply("1").marshal()

		require.NoError(t, err)
		assert.Equal(t,
			`<rpc-reply xmlns="`+BaseNamespace+`" message-id="1"><ok></ok></rpc-reply>`,
			string(data))
	})

	t.Run("data", func(t *testing.T) {
		reply := newReply("2")
		reply.Data = ops.NewElement("data").Append(
			&ops.Element{Space: "urn:sim:device", Name: "system-info", Children: []*ops.Element{
				{Name: "device-id", Text: "sim-001"},
			}},
		)

		data, err := reply.marshal()

		require.NoError(t, err)
		assert.Contains(t, string(data), `<rpc-reply xmlns="`+BaseNamespace+`" message-id="2">`)
		assert.Contains(t, string(data), `<data><system-info xmlns="urn:sim:device"><device-id>sim-001</device-id></system-info></data>`)
	})

	t.Run("protocol error", func(t *testing.T) {
		data, err := errorReply("3", ops.InvalidValue("tx-power 999 out of range")).marshal()

		require.NoError(t, err)
		text := string(data)
		assert.Contains(t, text, "<error-type>protocol</error-type>")
		assert.Contains(t, text, "<error-tag>invalid-value</error-tag>")
		assert.Contains(t, text, "<error-severity>error</error-severity>")
		assert.Contains(t, text, "<error-message>tx-power 999 out of range</error-message>")
	})

	t.Run("plain error becomes operation-failed", func(t *testing.T) {
		text := string(mustMarshal(t, errorReplyFrom("4", assert.AnError)))

		assert.Contains(t, text, "<error-type>application</error-type>")
		assert.Contains(t, text, "<error-tag>operation-failed</error-tag>")
	})

	t.Run("ops error keeps its tag", func(t *testing.T) {
		text := string(mustMarshal(t, errorReplyFrom("5", ops.DataMissing("interfaces/interface[name=eth9]"))))

		assert.Contains(t, text, "<error-type>protocol</error-type>")
		assert.Contains(t, text, "<error-tag>data-missing</error-tag>")
	})
}

func TestErrorReplyFromUnwrapsErrors(t *testing.T) {
	wrapped := requireWrap(ops.InvalidValue("bad %s", "value"))

	text := string(mustMarshal(t, errorReplyFrom("6", wrapped)))

	assert.Contains(t, text, "<error-tag>invalid-value</error-tag>")
	assert.Contains(t, text, "<error-message>bad value</error-message>")
}

// requireWrap wraps err with fmt.Errorf so the test proves errors.As works
// through wrapping.
func requireWrap(err error) error {
	return fmt.Errorf("operation failed: %w", err)
}

func mustMarshal(t *testing.T, reply rpcReply) []byte {
	t.Helper()
	data, err := reply.marshal()
	require.NoError(t, err)
	return data
}

// readReply reads one reply from the session and parses it.
func readReply(t *testing.T, io *sessionIO, mode framingMode) *ops.Element {
	t.Helper()

	data, err := newMessageReader(io.reader, mode).ReadMessage()
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(string(data), "<hello"), "unexpected server hello")

	element, err := ops.ParseElement(bytes.TrimSpace(data))
	require.NoError(t, err)
	return element
}

// errorTag returns the error-tag of a reply.
func errorTag(t *testing.T, reply *ops.Element) string {
	t.Helper()

	rpcError := reply.Child("rpc-error")
	require.NotNil(t, rpcError, "reply must carry an rpc-error")
	tag := rpcError.Child("error-tag")
	require.NotNil(t, tag)
	return tag.TrimmedText()
}

func TestGetConfigOverSSH(t *testing.T) {
	tests := []struct {
		name      string
		mode      framingMode
		helloMode framingMode
		helloCaps []string
	}{
		{
			name:      "end-of-message",
			mode:      framingEOM,
			helloMode: framingEOM,
			helloCaps: []string{CapabilityBase10},
		},
		{
			name:      "chunked",
			mode:      framingChunked,
			helloMode: framingChunked,
			helloCaps: []string{CapabilityBase11, CapabilityBase10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, st := newTestStore(t)
			srv := startTestServer(t, r, st, nil)
			io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))
			sendHello(t, io, tt.helloMode, tt.helloCaps...)

			send(t, io, tt.mode, `<rpc message-id="7"><get-config><source><running/></source>`+
				`<filter><system-info><device-id/></system-info></filter></get-config></rpc>`)

			reply := readReply(t, io, tt.mode)
			assert.Equal(t, "7", mustAttr(t, reply, "message-id"))

			data := reply.Child("data")
			require.NotNil(t, data)
			systemInfo := data.Child("system-info")
			require.NotNil(t, systemInfo)
			assert.Equal(t, "sim-001", systemInfo.Child("device-id").TrimmedText())
		})
	}
}

func TestEditConfigOverSSHReportsInvalidValue(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)
	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))
	sendHello(t, io, framingEOM, CapabilityBase10)

	send(t, io, framingEOM, `<rpc message-id="8"><edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>radio0</name><radio-link><tx-power>999</tx-power></radio-link></interface></interfaces>`+
		`</config></edit-config></rpc>`)

	reply := readReply(t, io, framingEOM)
	assert.Equal(t, "8", mustAttr(t, reply, "message-id"))
	assert.Equal(t, ops.TagInvalidValue, errorTag(t, reply))

	// A rejected edit leaves the candidate untouched.
	value, err := st.Get(context.Background(), store.Candidate, "interfaces/interface[name=radio0]/radio-link/tx-power")
	require.NoError(t, err)
	assert.Equal(t, 20.0, value)
}

func TestCloseSessionRepliesOkAndEndsTheSession(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)
	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))

	// base 1.0 only, so the session keeps end-of-message framing.
	sendHello(t, io, framingEOM, CapabilityBase10)
	send(t, io, framingEOM, `<rpc message-id="1"><close-session/></rpc>`)

	reply := readReply(t, io, framingEOM)
	assert.Equal(t, "rpc-reply", reply.Name)
	assert.Equal(t, "1", mustAttr(t, reply, "message-id"))
	require.NotNil(t, reply.Child("ok"))

	expectSessionClosed(t, io)
}

func TestUnknownOperationIsRejectedAndKeepsTheSession(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)
	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))

	sendHello(t, io, framingEOM, CapabilityBase10)
	send(t, io, framingEOM, `<rpc message-id="2"><get/></rpc>`)

	reply := readReply(t, io, framingEOM)
	assert.Equal(t, "2", mustAttr(t, reply, "message-id"))
	assert.Equal(t, ops.TagOperationNotSupported, errorTag(t, reply))

	// The session survives, so a close-session still works.
	send(t, io, framingEOM, `<rpc message-id="3"><close-session/></rpc>`)
	closeReply := readReply(t, io, framingEOM)
	assert.Equal(t, "3", mustAttr(t, closeReply, "message-id"))
	require.NotNil(t, closeReply.Child("ok"))
}

func TestMalformedMessagesEndTheSession(t *testing.T) {
	tests := []struct {
		name string
		body string
		tag  string
	}{
		{
			name: "malformed xml",
			body: `<rpc message-id="1"><close-session>`,
			tag:  ops.TagMalformedMessage,
		},
		{
			name: "missing message-id",
			body: `<rpc><close-session/></rpc>`,
			tag:  ops.TagMissingAttribute,
		},
		{
			name: "blank message-id",
			body: `<rpc message-id=""><close-session/></rpc>`,
			tag:  ops.TagMissingAttribute,
		},
		{
			name: "not an rpc",
			body: `<hello><capabilities/></hello>`,
			tag:  ops.TagMalformedMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, st := newTestStore(t)
			srv := startTestServer(t, r, st, nil)
			io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))

			sendHello(t, io, framingEOM, CapabilityBase10)
			send(t, io, framingEOM, tt.body)

			reply := readReply(t, io, framingEOM)
			assert.Equal(t, tt.tag, errorTag(t, reply))

			expectSessionClosed(t, io)
		})
	}
}

// mustAttr returns an attribute of a reply or fails.
func mustAttr(t *testing.T, element *ops.Element, name string) string {
	t.Helper()

	value, ok := element.Attr(name)
	require.True(t, ok, "attribute %s must be present on %s", name, element.Name)
	return value
}
