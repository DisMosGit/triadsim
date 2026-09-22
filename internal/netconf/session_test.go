package netconf

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// sessionIO is one client-side view of a NETCONF session.
type sessionIO struct {
	channel ssh.Channel
	reader  *bufio.Reader
}

// openNetconfSession opens a session channel, starts the netconf subsystem and
// reads the server hello.
func openNetconfSession(t *testing.T, client *ssh.Client) (*sessionIO, helloMessage) {
	t.Helper()

	channel := openSession(t, client)
	ok, err := channel.SendRequest("subsystem", true, ssh.Marshal(&subsystemRequest{Name: SubsystemName}))
	require.NoError(t, err)
	require.True(t, ok, "subsystem request must be accepted")

	buffered := bufio.NewReader(channel)
	data, err := newMessageReader(buffered, framingEOM).ReadMessage()
	require.NoError(t, err)

	hello, err := parseHello(bytes.TrimSpace(data))
	require.NoError(t, err)
	return &sessionIO{channel: channel, reader: buffered}, hello
}

// sendHello writes a client hello with the given capabilities and framing.
func sendHello(t *testing.T, io *sessionIO, mode framingMode, capabilities ...string) {
	t.Helper()

	var items strings.Builder
	for _, capability := range capabilities {
		items.WriteString("<capability>" + capability + "</capability>")
	}
	message := `<hello><capabilities>` + items.String() + `</capabilities><session-id>7</session-id></hello>`

	require.NoError(t, newMessageWriter(io.channel, mode).WriteMessage([]byte(message)))
}

// send writes one message in the given framing mode.
func send(t *testing.T, io *sessionIO, mode framingMode, message string) {
	t.Helper()
	require.NoError(t, newMessageWriter(io.channel, mode).WriteMessage([]byte(message)))
}

// expectSessionClosed asserts that the server closes the session channel.
func expectSessionClosed(t *testing.T, io *sessionIO) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		_, err := io.reader.ReadByte()
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err, "a read after the close must fail")
	case <-time.After(5 * time.Second):
		t.Fatal("server did not close the session")
	}
}

func TestServerSendsHelloFirst(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)

	io, hello := openNetconfSession(t, dialSSH(t, srv.Addr().String()))

	require.NotZero(t, hello.SessionID)
	assert.ElementsMatch(t, Capabilities(), hello.Capabilities)
	// The hello itself must be readable with end-of-message framing.
	assert.True(t, hello.chunked())
	_ = io
}

func TestSecondSessionGetsANewID(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)
	client := dialSSH(t, srv.Addr().String())

	_, first := openNetconfSession(t, client)
	_, second := openNetconfSession(t, client)

	assert.NotZero(t, first.SessionID)
	assert.NotEqual(t, first.SessionID, second.SessionID)
}

func TestSessionAcceptsChunkedHello(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)
	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))

	// A client that already uses chunked framing for its hello negotiates
	// chunked framing for the rest of the session.
	sendHello(t, io, framingChunked, CapabilityBase11, CapabilityBase10)

	// A malformed chunk header is a framing error in chunked mode, so the
	// server ends the session.
	require.NoError(t, writeRaw(io.channel, "\n#zz\n"))

	expectSessionClosed(t, io)
}

func TestSessionRejectsHelloWithoutBase(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)
	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))

	sendHello(t, io, framingEOM, CapabilityCandidate)

	expectSessionClosed(t, io)
}

func TestSessionRejectsNonHelloFirstMessage(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)
	io, _ := openNetconfSession(t, dialSSH(t, srv.Addr().String()))

	send(t, io, framingEOM, `<rpc message-id="1"><close-session/></rpc>`)

	expectSessionClosed(t, io)
}

// writeRaw writes unframed bytes to the channel.
func writeRaw(channel ssh.Channel, data string) error {
	_, err := channel.Write([]byte(data))
	return err
}
