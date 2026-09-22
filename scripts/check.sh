#!/usr/bin/env bash
#
# Local pre-flight check: formatting, vet and unit tests.
# Integration tests (testcontainers, build tag "integration") are not run here;
# they land in Phase 6 and will need Docker.
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

echo "==> go test"
go test ./...

echo "OK"
