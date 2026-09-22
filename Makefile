# TriadSim developer entry points.
#
# GO and CONFIG can be overridden, for example:
#   make run CONFIG=configs/local.yaml

GO     ?= go
CONFIG ?= configs/default.yaml

.PHONY: build test lint run demo clean

# build compiles every package, including cmd/simulator.
build:
	$(GO) build ./...

# test runs the unit tests; integration tests are gated by the "integration" build tag.
test:
	$(GO) test ./...

# lint fails on unformatted files and on go vet findings.
lint:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: these files need formatting:" >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi
	$(GO) vet ./...

# run starts the simulator and blocks until SIGINT/SIGTERM.
run:
	$(GO) run ./cmd/simulator start --config $(CONFIG)

# demo reproduces the cross-domain scenario; until Phase 6 it reports that it is not implemented.
demo:
	./scripts/demo.sh

# clean removes build and coverage artifacts.
clean:
	rm -rf bin coverage.out coverage.html
