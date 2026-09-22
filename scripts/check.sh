#!/usr/bin/env bash
#
# Local pre-flight check: formatting, vet, unit tests and — when Docker is
# available — the container integration tests.
#
# The integration tests build the simulator, run it in alpine:3.20 and drive it
# over the network, so they need a Docker daemon. Without one they are skipped
# with a notice instead of failing: `make lint && make test` stays usable on a
# machine that cannot run Docker.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

echo "==> gofmt"
unformatted="$(gofmt -l .)"
if [[ -n "${unformatted}" ]]; then
  echo "gofmt: these files need formatting:" >&2
  echo "${unformatted}" >&2
  exit 1
fi

echo "==> go vet"
go vet ./...
go vet -tags=integration ./...

echo "==> go test"
go test ./...

echo "==> integration tests (build tag integration)"
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  go test -tags=integration ./...
else
  echo "docker is not available: skipping go test -tags=integration ./..." >&2
fi

echo "OK"
