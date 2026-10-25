# Development guide

This document is for contributors building tDocs from source.  
**End users should install a release package** — see [README.md](README.md).

## Prerequisites

- Go **1.26+** (see `go.mod`)
- Make (optional convenience)
- Node.js only for `make lint` JS syntax checks (not required to run tDocs)
- Docker (optional) for image builds and local container tests
- `dpkg-deb` / `rpm` for local package builds

No CGO. The stack is pure Go (`modernc.org/sqlite`, `gotd/td`).

## Everyday commands

```bash
make build          # static ./tdocs with version metadata
make test           # go test ./...
make vet            # go vet ./...
make lint           # vet + gofmt check + node --check on embedded JS
make start          # build → setup if needed → login → server + browser
make doctor         # diagnostics
make run            # build + server
```

Direct Go:

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go vet ./...
gofmt -l cmd internal
```

## Version injection

Never hardcode release versions in source. Linker flags:

```bash
go build -ldflags "-X main.Version=2.1.0 -X main.Commit=$(git rev-parse --short HEAD) -X main.BuildDate=$(date -u +%Y-%m-%d)" ./cmd/tdocs
```

`make build` and CI do this automatically. Development builds print `tDocs dev`.

## Release packaging (local)

```bash
make release          # dist/linux/{amd64,arm64,armv7,386,ppc64le,s390x}/tdocs
make package-tar      # dist/tar/tdocs_<ver>_linux_<arch>.tar.gz
make package-deb      # dist/deb/<debarch>/tdocs_<ver>_<debarch>.deb
make package-rpm      # dist/rpm/<rpmarch>/tdocs_<ver>_<rpmarch>.rpm
make package-all      # all of the above + SHA256SUMS
```

Requirements:

| Package | Tool |
| :--- | :--- |
| `.tar.gz` | `tar` |
| `.deb` | `dpkg-deb` (Debian/Ubuntu: `dpkg-dev`) |
| `.rpm` | `rpmbuild` (Debian: `apt install rpm`; RHEL: `rpm-build`) |

Artifact layout:

```text
dist/
├── linux/<arch>/tdocs
├── tar/tdocs_<ver>_linux_<arch>.tar.gz
├── deb/<debarch>/tdocs_<ver>_<debarch>.deb
├── rpm/<rpmarch>/tdocs_<ver>_<rpmarch>.rpm
└── SHA256SUMS
```

`dist/` is gitignored.

## CI & releases

| Workflow | Trigger | Purpose |
| :--- | :--- | :--- |
| `.github/workflows/ci.yml` | push/PR | gofmt, vet, tests, static build, smoke, cross-build matrix |
| `.github/workflows/release.yml` | tag `v*` | full release: binaries → deb/rpm/tar → checksums → container smoke tests → GitHub Release |

Tag a release:

```bash
git tag v2.1.0
git push origin v2.1.0
```

Do not build release artifacts on developer machines for publication — CI is the source of truth.

## Project layout

```text
cmd/tdocs/          CLI entrypoint (version, setup, server, ops commands)
internal/app/       Config + path resolution (dev / user / system modes)
internal/db/        SQLite schema, migrations, snapshots
internal/telegram/  MTProto client, upload/download, discovery
internal/web/       HTTP server, handlers, embedded UI (//go:embed)
internal/crypto/    AES-GCM session encryption, Argon2id
internal/storage/   Backend interface (Telegram today)
packaging/systemd/  Production systemd unit
scripts/            build/package/install/checksum scripts
docs/adr/           Architecture decision records
```

## Configuration precedence

```text
real environment variables
    ↓
./.env  (source-tree / legacy)
    ↓
$TDOCS_CONFIG_DIR/.env  (or XDG / /etc/tdocs/.env)
    ↓
built-in defaults
```

Path modes (`TDOCS_MODE=dev|user|system`):

- **dev** — working directory (source checkout detection via `go.mod`)
- **user** — `~/.config/tdocs`, `~/.local/share/tdocs`
- **system** — `/etc/tdocs`, `/var/lib/tdocs` (packages + systemd unit force this)

## Tests

```bash
go test ./...
```

Integration-ish coverage lives in `internal/web`, `internal/db`, `internal/telegram`.  
Frontend: `node --check internal/web/static/app.js` (the file is embedded and served raw).

## Docker

```bash
docker compose up -d --build
docker compose exec tdocs tdocs login
```

See `Dockerfile` — multi-stage, `CGO_ENABLED=0`, Alpine runtime with CA bundle only.

## Security notes for contributors

- Never commit `.env`, `*.db`, `*.key`, sessions, or tokens.
- Keep data dirs `0700` and secret files `0600`.
- Do not log API hashes, passwords, or session material.
- Production packages must not embed secrets.
