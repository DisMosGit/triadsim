package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultSimulatorAddr is the RESTCONF base URL the commands that talk to a
// running simulator use when --addr is not given. It matches the default
// restconf.port of the configuration.
const DefaultSimulatorAddr = "http://127.0.0.1:8080"

// clientTimeout bounds one request to a running simulator, so a wedged server
// does not hang the command forever.
const clientTimeout = 10 * time.Second

// client is the HTTP client of the commands that talk to a running simulator.
var client = &http.Client{Timeout: clientTimeout}

// endpoint joins a simulator base URL with a request path.
func endpoint(addr, path string) string {
	for len(addr) > 0 && addr[len(addr)-1] == '/' {
		addr = addr[:len(addr)-1]
	}
	return addr + path
}

// postJSON sends a JSON request and returns the response body. A non-2xx
// status is an error that carries the response document.
func postJSON(ctx context.Context, url string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	return do(request)
}

// get sends a request with an Accept header and returns the response body.
func get(ctx context.Context, url, accept string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	return do(request)
}

// do performs a request and returns its body, turning a non-2xx status into an
// error.
func do(request *http.Request) (string, error) {
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", request.Method, request.URL, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", request.Method, request.URL, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("%s %s: %s: %s", request.Method, request.URL, response.Status,
			bytes.TrimSpace(body))
	}
	return string(body), nil
}
