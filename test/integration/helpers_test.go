//go:build integration

// Package integration runs the compiled simulator inside a container and drives
// it with real clients over the network. The tests need Docker; the alpine
// image they run in and the static simulator binary they mount need no registry
// access.
package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/ssh"
)

// image is the container image the simulator runs in. It carries no tools of
// its own: the command is the mounted binary.
const image = "alpine:3.20"

// startupTimeout bounds waiting for the simulator to listen.
const startupTimeout = 60 * time.Second

// httpClient is the client of the host-side probes.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// simulator is one running simulator container with its ports published on the
// host.
type simulator struct {
	container   testcontainers.Container
	snmpPort    uint16
	netconfAddr string
	restconfURL string
	metricsURL  string
}

var (
	buildOnce  sync.Once
	binaryPath string
	binaryErr  error
)

// simulatorBinary compiles the simulator once per test run into a static
// binary and returns its path.
func simulatorBinary(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "triadsim-integration-")
		if err != nil {
			binaryErr = err
			return
		}
		require.NoError(t, os.Chmod(dir, 0o755))

		binaryPath = filepath.Join(dir, "simulator")
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, "./cmd/simulator")
		cmd.Dir = filepath.Join("..", "..")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			binaryErr = fmt.Errorf("go build: %w: %s", err, out)
		}
	})
	require.NoError(t, binaryErr)
	return binaryPath
}

// startSimulator starts the simulator in a container and waits until RESTCONF
// answers. traps is the host port the trap receiver listens on; the container
// reaches it over the Docker host gateway.
func startSimulator(t *testing.T, trapPort int) *simulator {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	binary := simulatorBinary(t)

	dataDir := t.TempDir()
	require.NoError(t, os.Chmod(dataDir, 0o777))
	configDir := t.TempDir()
	require.NoError(t, os.Chmod(configDir, 0o755))

	configPath := filepath.Join(configDir, "config.yaml")
	config := fmt.Sprintf(`snmp:
  port: 1161
  trap-host: %s
  trap-port: %d
netconf:
  port: 1830
restconf:
  port: 8080
metrics:
  port: 9090
log:
  level: info
startup:
  file: /data/startup.json
`, testcontainers.HostInternal, trapPort)
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o644))

	request := testcontainers.ContainerRequest{
		Image: image,
		Cmd:   []string{"/simulator", "start", "--config", "/config.yaml"},
		Mounts: testcontainers.ContainerMounts{
			testcontainers.BindMount(binary, "/simulator"),
			testcontainers.BindMount(configPath, "/config.yaml"),
			testcontainers.BindMount(dataDir, "/data"),
		},
		ExposedPorts: []string{"1161/udp", "1830/tcp", "8080/tcp", "9090/tcp"},
		// The container reaches the host's trap receiver through the gateway.
		ExtraHosts: []string{testcontainers.HostInternal + ":host-gateway"},
		WaitingFor: wait.ForListeningPort("8080/tcp").WithStartupTimeout(startupTimeout),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: request,
		Started:          true,
	})
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, container)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	snmpPort := mappedPort(t, container, "1161/udp")
	netconfPort := mappedPort(t, container, "1830/tcp")
	restconfPort := mappedPort(t, container, "8080/tcp")
	metricsPort := mappedPort(t, container, "9090/tcp")

	return &simulator{
		container:   container,
		snmpPort:    snmpPort,
		netconfAddr: net.JoinHostPort(host, fmt.Sprint(netconfPort)),
		restconfURL: "http://" + net.JoinHostPort(host, fmt.Sprint(restconfPort)),
		metricsURL:  "http://" + net.JoinHostPort(host, fmt.Sprint(metricsPort)),
	}
}

// mappedPort returns the host port a container port is published on.
func mappedPort(t *testing.T, container testcontainers.Container, port string) uint16 {
	t.Helper()

	mapped, err := container.MappedPort(context.Background(), port)
	require.NoError(t, err)
	return mapped.Num()
}

// trapReceiver is a UDP socket on every host interface, which is how the
// container reaches it through the Docker gateway.
type trapReceiver struct {
	conn net.PacketConn
	port int
}

// newTrapReceiver binds an ephemeral port on 0.0.0.0.
func newTrapReceiver(t *testing.T) *trapReceiver {
	t.Helper()

	conn, err := net.ListenPacket("udp", "0.0.0.0:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &trapReceiver{conn: conn, port: conn.LocalAddr().(*net.UDPAddr).Port}
}

// trapOID waits for one trap and returns its snmpTrapOID.0 value.
func (r *trapReceiver) trapOID(t *testing.T) string {
	t.Helper()

	require.NoError(t, r.conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	buf := make([]byte, 65535)
	n, _, err := r.conn.ReadFrom(buf)
	require.NoError(t, err)

	packet, err := (&gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: "public"}).SnmpDecodePacket(buf[:n])
	require.NoError(t, err)
	for _, variable := range packet.Variables {
		if strings.TrimPrefix(variable.Name, ".") != "1.3.6.1.2.1.11.4.1.0" {
			continue
		}
		value, ok := variable.Value.(string)
		require.True(t, ok, "snmpTrapOID.0 is an ObjectIdentifier")
		return strings.TrimPrefix(value, ".")
	}
	require.Fail(t, "the trap carries no snmpTrapOID.0 varbind")
	return ""
}

// post sends a simulation request and returns its status.
func post(t *testing.T, url, body string) int {
	t.Helper()

	response, err := httpClient.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

// getBody fetches a URL and returns its body, or an empty string while the
// endpoint is not answering yet.
func getBody(url string) string {
	response, err := httpClient.Get(url)
	if err != nil {
		return ""
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return ""
	}
	return string(body)
}

// ptpState reads the PTP clock state, or returns an empty string while the
// endpoint is not answering yet.
func ptpState(base string) string {
	var document map[string]any
	if err := json.Unmarshal([]byte(getBody(base+"/restconf/data/sim-sync:ptp/clock/state")), &document); err != nil {
		return ""
	}
	state, _ := document["sim-sync:state"].(string)
	return state
}

// netconfClient is a minimal EOM-framed NETCONF client over an SSH channel.
type netconfClient struct {
	channel ssh.Channel
	reader  *bufio.Reader
}

// dialNETCONF opens the netconf SSH subsystem and exchanges the hello.
func dialNETCONF(t *testing.T, addr string) *netconfClient {
	t.Helper()

	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "admin",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	channel, requests, err := client.OpenChannel("session", nil)
	require.NoError(t, err)
	go ssh.DiscardRequests(requests)
	t.Cleanup(func() { _ = channel.Close() })

	accepted, err := channel.SendRequest("subsystem", true, ssh.Marshal(&struct{ Name string }{Name: "netconf"}))
	require.NoError(t, err)
	require.True(t, accepted, "the server must accept the netconf subsystem")

	netconf := &netconfClient{channel: channel, reader: bufio.NewReader(channel)}
	require.Contains(t, netconf.readMessage(t), "<hello")
	_, err = channel.Write([]byte(`<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">` +
		`<capabilities><capability>urn:ietf:params:netconf:base:1.0</capability></capabilities></hello>]]>]]>`))
	require.NoError(t, err)
	return netconf
}

// readMessage reads one end-of-message framed document.
func (c *netconfClient) readMessage(t *testing.T) string {
	t.Helper()

	var message strings.Builder
	for !strings.HasSuffix(message.String(), "]]>]]>") {
		b, err := c.reader.ReadByte()
		require.NoError(t, err)
		message.WriteByte(b)
	}
	return message.String()
}

// exchange sends one RPC and returns its reply.
func (c *netconfClient) exchange(t *testing.T, rpc string) string {
	t.Helper()

	_, err := c.channel.Write([]byte(rpc + "]]>]]>"))
	require.NoError(t, err)
	return c.readMessage(t)
}
