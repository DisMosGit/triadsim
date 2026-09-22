//go:build tools

// Package tools pins the approved dependency stack in go.mod.
//
// Every entry of the closed list in AGENTS.md is imported here, even when the
// simulator already imports it, so the list stays checkable in one place and
// `go mod tidy` can never drop a pin. The build tag excludes this file from
// every real build.
package tools

import (
	_ "github.com/go-chi/chi/v5"
	_ "github.com/gosnmp/gosnmp"
	_ "github.com/openconfig/gnmi/proto/gnmi"
	_ "github.com/prometheus/client_golang/prometheus"
	_ "github.com/prometheus/client_golang/prometheus/promhttp"
	_ "github.com/spf13/cobra"
	_ "github.com/stretchr/testify/assert"
	_ "github.com/stretchr/testify/require"
	_ "github.com/testcontainers/testcontainers-go"
	_ "golang.org/x/crypto/ssh"
	_ "google.golang.org/grpc"
	_ "gopkg.in/yaml.v3"
)
