package netconf

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/DisMosGit/triadsim/internal/store"
)

// dialSSH connects to addr with no credentials: the server accepts anything.
func dialSSH(t *testing.T, addr string, auth ...ssh.AuthMethod) *ssh.Client {
	t.Helper()

	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	require.NoError(t, err, "dial %s", addr)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// openSession opens a raw session channel and drains its requests.
func openSession(t *testing.T, client *ssh.Client) ssh.Channel {
	t.Helper()

	channel, requests, err := client.OpenChannel("session", nil)
	require.NoError(t, err)
	go ssh.DiscardRequests(requests)
	t.Cleanup(func() { _ = channel.Close() })
	return channel
}

func TestListenRejectsMissingDependencies(t *testing.T) {
	srv := New(nil, store.NewMemory(store.Options{}), nil, Options{Addr: "127.0.0.1:0"})
	assert.ErrorContains(t, srv.Listen(), "router is nil")

	r, _ := newTestStore(t)
	srv = New(r, nil, nil, Options{Addr: "127.0.0.1:0"})
	assert.ErrorContains(t, srv.Listen(), "store is nil")
}

func TestServeRequiresListen(t *testing.T) {
	r, st := newTestStore(t)
	srv := New(r, st, nil, Options{Addr: "127.0.0.1:0"})

	assert.Nil(t, srv.Addr())
	assert.ErrorContains(t, srv.Serve(context.Background()), "not listening")
}

func TestAddrIsEphemeralOnRequest(t *testing.T) {
	r, st := newTestStore(t)
	srv := New(r, st, nil, Options{Addr: "127.0.0.1:0"})
	require.NoError(t, srv.Listen())
	t.Cleanup(func() { _ = srv.Close() })

	addr, ok := srv.Addr().(*net.TCPAddr)
	require.True(t, ok)
	assert.Equal(t, "127.0.0.1", addr.IP.String())
	assert.NotZero(t, addr.Port)
}

func TestServeStopsOnClose(t *testing.T) {
	r, st := newTestStore(t)
	srv := New(r, st, nil, Options{Addr: "127.0.0.1:0"})
	require.NoError(t, srv.Listen())

	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background()) }()

	require.NoError(t, srv.Close())
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Close")
	}
}

func TestSubsystemAcceptsOnlyNetconf(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)

	client := dialSSH(t, srv.Addr().String())

	accepted := openSession(t, client)
	ok, err := accepted.SendRequest("subsystem", true, ssh.Marshal(&subsystemRequest{Name: SubsystemName}))
	require.NoError(t, err)
	assert.True(t, ok)

	rejected := openSession(t, client)
	ok, err = rejected.SendRequest("subsystem", true, ssh.Marshal(&subsystemRequest{Name: "sftp"}))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestNonSessionChannelIsRejected(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)

	client := dialSSH(t, srv.Addr().String())
	payload := ssh.Marshal(&struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}{Host: "127.0.0.1", Port: 22})

	_, _, err := client.OpenChannel("direct-tcpip", payload)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "only session channels")
}

func TestAuthenticationAcceptsAnyCredentials(t *testing.T) {
	r, st := newTestStore(t)
	srv := startTestServer(t, r, st, nil)
	addr := srv.Addr().String()

	// No auth method at all ("none") and a password both succeed.
	none := dialSSH(t, addr)
	require.NotNil(t, none)

	password := dialSSH(t, addr, ssh.Password("whatever"))
	session := openSession(t, password)
	ok, err := session.SendRequest("subsystem", true, ssh.Marshal(&subsystemRequest{Name: SubsystemName}))
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestInjectedHostKeyIsUsed(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(private)
	require.NoError(t, err)

	r, st := newTestStore(t)
	srv := New(r, st, nil, Options{Addr: "127.0.0.1:0", HostKey: signer})
	require.NoError(t, srv.Listen())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	var seen ssh.PublicKey
	config := &ssh.ClientConfig{
		User: "admin",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = key
			return nil
		},
		Timeout: 5 * time.Second,
	}
	client, err := ssh.Dial("tcp", srv.Addr().String(), config)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	require.NotNil(t, seen)
	assert.Equal(t, ssh.FingerprintSHA256(signer.PublicKey()), ssh.FingerprintSHA256(seen))
}
