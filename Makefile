# tDocs — development and release build targets.
#
# End users do NOT need Make: install a release package and run `tdocs`.
# Make is for developers and CI only.

GO ?= go
BINARY := tdocs
PKG := ./cmd/tdocs
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%d)
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)

.PHONY: build build-all test vet lint fmt setup start doctor run clean \
        release package-deb package-rpm package-tar package-all checksums \
        docker-up docker-down help

## Development ---------------------------------------------------------------

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) $(PKG)

build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o dist/linux/amd64/tdocs $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o dist/linux/arm64/tdocs $(PKG)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o dist/darwin/arm64/tdocs $(PKG)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o dist/windows/amd64/tdocs.exe $(PKG)

test:
	CGO_ENABLED=0 $(GO) test ./...

vet:
	CGO_ENABLED=0 $(GO) vet ./...

fmt:
	gofmt -w cmd internal

# vet + gofmt check + frontend syntax. The embedded app.js is served straight
# to the browser, so a single syntax error would silently blank the dashboard.
lint:
	CGO_ENABLED=0 $(GO) vet ./...
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	@if command -v node >/dev/null 2>&1; then \
	  node --check internal/web/static/app.js && node --check bin/tdocs.js; \
	else \
	  echo "  ○ node not found — skipping JS syntax checks"; \
	fi

setup: build
	./$(BINARY) setup

start: build
	./$(BINARY) start

doctor: build
	./$(BINARY) doctor

run: build
	./$(BINARY) server

## Release (developers/CI only — users install packages) ---------------------

# Build static binaries for all supported Linux architectures into dist/linux/.
release:
	scripts/build-release.sh $(VERSION) dist

# Universal tarball for the host architecture (or ARCH=arm64 make package-tar).
ARCH ?= $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/' -e 's/armv7l/armv7/')
package-tar: release
	scripts/package-tar.sh $(VERSION) $(ARCH)

package-deb: release
	scripts/package-deb.sh $(VERSION) amd64
	scripts/package-deb.sh $(VERSION) arm64

package-rpm: release
	scripts/package-rpm.sh $(VERSION) amd64
	scripts/package-rpm.sh $(VERSION) arm64

package-all: release
	scripts/package-tar.sh $(VERSION) amd64
	scripts/package-tar.sh $(VERSION) arm64
	scripts/package-deb.sh $(VERSION) amd64
	scripts/package-deb.sh $(VERSION) arm64
	scripts/package-rpm.sh $(VERSION) amd64
	scripts/package-rpm.sh $(VERSION) arm64
	$(MAKE) checksums

checksums:
	scripts/checksums.sh dist

## Docker --------------------------------------------------------------------

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -f $(BINARY) $(BINARY).exe
	rm -rf dist

help:
	@echo "  make build        static $(BINARY) with version metadata"
	@echo "  make test|vet|lint  quality gates"
	@echo "  make start        setup if needed → login → server + browser"
	@echo "  make doctor       config/db/session/port diagnostics"
	@echo "  make release      all Linux binaries → dist/linux/"
	@echo "  make package-deb  .deb for amd64 + arm64"
	@echo "  make package-rpm  .rpm for x86_64 + aarch64"
	@echo "  make package-tar  universal .tar.gz for ARCH"
	@echo "  make package-all  tar + deb + rpm + SHA256SUMS"
	@echo "  make docker-up    docker compose up -d --build"
