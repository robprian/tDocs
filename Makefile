# tDocs — local build outputs to the repo root (./tdocs).
# Paling gampang:  make start   (build → setup bila perlu → login → server + browser)

GO ?= go
BINARY := tdocs
PKG := ./cmd/tdocs

.PHONY: build build-all test vet lint setup start doctor run clean docker-up docker-down help

build:
	CGO_ENABLED=0 $(GO) build -o $(BINARY) $(PKG)

build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -o dist/$(BINARY)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -o dist/$(BINARY)-linux-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -o dist/$(BINARY)-darwin-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -o dist/$(BINARY)-windows-amd64.exe $(PKG)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# vet + frontend syntax checks. The embedded app.js is served straight to the
# browser, so a single syntax error would silently blank the whole dashboard.
lint:
	CGO_ENABLED=0 $(GO) vet ./...
	node --check internal/web/static/app.js
	node --check bin/tdocs.js

# Wizard konfigurasi sekali saja (API_ID/HASH + password + port → .env).
setup: build
	./$(BINARY) setup

# Satu perintah untuk jalan: setup bila perlu → login → browser → server.
start: build
	./$(BINARY) start

# Diagnosa config/db/session/port + saran perbaikan.
doctor: build
	./$(BINARY) doctor

run: build
	./$(BINARY) server

# Docker: salin .env.example → .env dan isi kredensial dulu.
docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -f $(BINARY) $(BINARY).exe
	rm -rf dist

help:
	@echo "  make lint       go vet + node --check on embedded frontend"
	@echo "  make setup      wizard .env sekali saja"
	@echo "  make start      jalan pintas (setup → login → server + browser)"
	@echo "  make doctor     cek config/db/session/port"
	@echo "  make run        build + server langsung"
	@echo "  make docker-up  jalan via Docker Compose"
