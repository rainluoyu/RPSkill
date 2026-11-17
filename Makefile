GO ?= go
NPM ?= npm
VERSION ?= 1.0.0-rc.1
GOFLAGS ?=
DIST_DIR ?= dist
LDFLAGS = -s -w -X github.com/luoyu/rpskill/internal/server.Version=$(VERSION)
BINARY = build/rps

.PHONY: help build test vet build-web sync-web release package verify-release smoke-release test-release cross clean
help:
	@printf '%s\n' 'build: build Vue assets and the native single binary' 'test: Go tests (no opt-in real model tests)' 'vet: Go static checks' 'build-web: npm ci, Vue build and embed synchronization' 'release/package: rebuild UI + six candidate packages in dist/VERSION (must not exist)' 'verify-release: verify archive paths, modes, hashes, manifests and executable architectures' 'smoke-release: Linux package outside repository, no Go/Node at runtime or real model' 'test-release: release-tool unit tests' 'cross: Linux/macOS/Windows amd64+arm64 bare binaries' 'clean: remove generated build binaries (keeps dist releases)'

build: build-web
	@mkdir -p build
	CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 $(GO) build $(GOFLAGS) -mod=readonly -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/rps

test:
	$(GO) test $(GOFLAGS) ./...

vet:
	$(GO) vet $(GOFLAGS) ./...

build-web:
	cd web && $(NPM) ci && $(NPM) run build
	./scripts/sync-web.sh

sync-web:
	./scripts/sync-web.sh

release: build
	$(GO) run $(GOFLAGS) ./scripts/release --version '$(VERSION)' --output '$(DIST_DIR)' --go '$(GO)'

package: release

verify-release:
	$(GO) run $(GOFLAGS) ./scripts/release --verify '$(DIST_DIR)/$(VERSION)'

smoke-release:
	$(GO) run $(GOFLAGS) ./scripts/release --smoke '$(DIST_DIR)/$(VERSION)'

test-release:
	$(GO) test $(GOFLAGS) ./scripts/release

cross: build-web
	@set -eu; mkdir -p build; \
	for os in linux darwin windows; do \
	  for arch in amd64 arm64; do \
	    ext=''; if [ "$$os" = windows ]; then ext='.exe'; fi; \
	    CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 GOOS=$$os GOARCH=$$arch $(GO) build $(GOFLAGS) -mod=readonly -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o "build/rps-$$os-$$arch$$ext" ./cmd/rps; \
	  done; \
	done

clean:
	rm -rf build
