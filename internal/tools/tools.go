//go:build tools

// Package tools pins the approved dependency stack in go.mod.
//
// Some of these libraries are not imported by simulator code yet — gosnmp,
// SSH, chi, Prometheus and testcontainers arrive with Phases 1-6 — but a blank
// import here keeps `go mod tidy` from dropping them, so go.mod always matches
// the closed list in AGENTS.md. The build tag excludes this file from every
// real build.
package tools

import (
	_ "github.com/go-chi/chi/v5"
	_ "github.com/gosnmp/gosnmp"
	_ "github.com/prometheus/client_golang/prometheus"
	_ "github.com/prometheus/client_golang/prometheus/promhttp"
	_ "github.com/spf13/cobra"
	_ "github.com/stretchr/testify/assert"
	_ "github.com/stretchr/testify/require"
	_ "github.com/testcontainers/testcontainers-go"
	_ "golang.org/x/crypto/ssh"
	_ "gopkg.in/yaml.v3"
)
