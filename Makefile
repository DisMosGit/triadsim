# TriadSim developer entry points.
#
# GO and CONFIG can be overridden, for example:
#   make run CONFIG=configs/local.yaml

GO     ?= go
CONFIG ?= configs/default.yaml

.PHONY: build test integration lint check run demo clean

# build compiles every package, including cmd/simulator.
build:
	$(GO) build ./...

# test runs the unit tests; integration tests are gated by the "integration" build tag.
test:
	$(GO) test ./...

# integration runs the testcontainers integration tests; Docker is required.
integration:
	$(GO) test -tags=integration ./...

# lint fails on unformatted files and on go vet findings.
lint:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: these files need formatting:" >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi
	$(GO) vet ./...
	$(GO) vet -tags=integration ./...

# check runs scripts/check.sh: gofmt, vet, unit tests and, with Docker, the
# integration tests.
check:
	./scripts/check.sh

# run starts the simulator and blocks until SIGINT/SIGTERM.
run:
	$(GO) run ./cmd/simulator start --config $(CONFIG)

# demo reproduces the cross-domain scenario of docs/demo.md in one command.
# DEMO_NETCONF=1 also captures the NETCONF notifications.
demo:
	./scripts/demo.sh

# clean removes build and coverage artifacts.
clean:
	rm -rf bin coverage.out coverage.html
