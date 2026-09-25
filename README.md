<div align="center">

# ⚡ tDocs

**Cloud storage pribadi bertenaga Telegram MTProto — single binary, dashboard web, API siap embed.**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Pure Go](https://img.shields.io/badge/CGO-Zero%20(Pure%20Go)-orange)](https://modernc.org/sqlite)
[![Release](https://img.shields.io/github/v/release/robprian/tDocs)](https://github.com/robprian/tDocs/releases)

</div>

> **English:** tDocs turns your Telegram account into a secure personal cloud drive: dark-first dashboard with storage analytics, resumable uploads, HTTP 206 streaming, share links, trash + versions, automated snapshots, and a public CDN + OpenAPI for embedding files anywhere.
>
> **You do not need Go, Node.js, npm, or a Git clone to run tDocs.** Download a package, install it, run `tdocs`.

---

## ⬇️ Download tDocs

Grab the latest release: **[github.com/robprian/tDocs/releases](https://github.com/robprian/tDocs/releases)**

| Your system | Download |
| :--- | :--- |
| **Debian / Ubuntu / Mint / Pop!_OS** | `tdocs_<version>_amd64.deb` or `_arm64.deb` |
| **RHEL / Rocky / Alma / CentOS Stream / Fedora** | `tdocs_<version>_x86_64.rpm` or `_aarch64.rpm` |
| **openSUSE Leap / Tumbleweed / SUSE** | `tdocs_<version>_x86_64.rpm` (RPM format) |
| **Any Linux (universal)** | `tdocs_<version>_linux_<arch>.tar.gz` |

Verify downloads with `SHA256SUMS` published on the same release page.

**Tested on** (package install + `tdocs version` + server smoke test in CI containers):

- Ubuntu LTS (GitHub-hosted runner)
- Debian stable (container)
- Rocky Linux 9 (container)
- openSUSE Leap 15.6 (container)

Other distributions with a compatible glibc userspace may also work. The primary binary is **statically linked** (pure Go: `modernc.org/sqlite` + `gotd`) — no dynamic library dependencies and no compiler toolchain required at runtime.

---

## 📦 Install

### Debian / Ubuntu

```bash
sudo apt install ./tdocs_2.1.0_amd64.deb
sudo systemctl enable --now tdocs
```

### RHEL / Rocky / Alma / Fedora

```bash
sudo dnf install ./tdocs_2.1.0_x86_64.rpm
sudo systemctl enable --now tdocs
```

### openSUSE / SUSE

```bash
sudo zypper install ./tdocs_2.1.0_x86_64.rpm
# or: sudo rpm -i ./tdocs_2.1.0_x86_64.rpm
```

### Universal tarball

```bash
tar -xzf tdocs_2.1.0_linux_amd64.tar.gz
cd tdocs
sudo ./install.sh          # system install + systemd (or --user for per-user)
```

Without root, `./install.sh` installs to `~/.local/bin` with user-mode paths.

### Docker (optional)

```bash
cp .env.example .env   # isi TDOCS_TG_APP_ID + TDOCS_TG_APP_HASH
docker compose up -d --build
docker compose exec tdocs tdocs login
# buka http://localhost:8080
```

---

## 🚀 First run

```bash
tdocs setup    # wizard: Telegram API_ID/HASH + password + port
tdocs login    # pairing Telegram (OTP + 2FA) — sekali saja
tdocs start    # buka browser → dashboard
```

Bare `tdocs` behaves like `tdocs start`: if the app is not configured yet, the setup wizard launches automatically.

Needs `API_ID` + `API_HASH` from [my.telegram.org](https://my.telegram.org) → API development tools (once).

**Dashboard:** http://localhost:8080  
**Logs (service):** `sudo journalctl -u tdocs -f`  
**Diagnostics:** `tdocs doctor` · `tdocs status` · `tdocs version`

---

## 🗂️ Where data lives

| Mode | Config | Data (DB, key, session) |
| :--- | :--- | :--- |
| **User install** | `~/.config/tdocs/` | `~/.local/share/tdocs/` |
| **System install** (package/service) | `/etc/tdocs/` | `/var/lib/tdocs/` |
| **Source checkout (dev)** | `./.env` | `./tdocs.db` |

Override anytime:

```bash
TDOCS_CONFIG_DIR=/custom/config
TDOCS_DATA_DIR=/custom/data
TDOCS_DB_PATH=/custom/data/tdocs.db
```

Precedence: **CLI/environment → production `.env` → defaults**.  
Legacy `ROBDOCS_*` / `TELEDRIVE_*` variables and old databases (`robdocs.db`, `teledrive.db`) keep working.

Directories are created `0700`; the database and `.tdocs.key` are `0600`. Production installs never write into `/usr/bin` or the extracted tarball folder.

---

## ⚙️ System service

Packages ship `tdocs.service` (runs as dedicated user `tdocs`, hardened unit, graceful shutdown with final snapshot):

```bash
sudo systemctl enable tdocs
sudo systemctl start tdocs
sudo systemctl status tdocs
sudo systemctl restart tdocs
sudo journalctl -u tdocs -f
```

From a tarball install:

```bash
tdocs service install          # system unit (root)
tdocs service install --user   # per-user unit
tdocs service remove
```

---

## 🔧 Configuration

| Variable | Default | Description |
| :--- | :--- | :--- |
| `TDOCS_PORT` / `TDOCS_HOST` | `8080` / `0.0.0.0` | Dashboard bind |
| `TDOCS_DB_PATH` | `<data>/tdocs.db` | SQLite path |
| `TDOCS_CONFIG_DIR` | XDG / `/etc/tdocs` | Config directory (holds `.env`) |
| `TDOCS_DATA_DIR` | XDG / `/var/lib/tdocs` | Data directory |
| `TDOCS_ADMIN_PASSWORD` | `admin123` | Dashboard + Bearer API key |
| `TDOCS_SECRET_KEY` | auto (`.tdocs.key`) | MTProto session encryption |
| `TDOCS_TG_APP_ID` / `TDOCS_TG_APP_HASH` | *(wizard)* | From my.telegram.org |
| `TDOCS_CDN_PUBLIC` / `TDOCS_CDN_BASE_URL` | `true` / host | Public CDN behaviour |
| `TDOCS_TLS_CERT_FILE` / `TDOCS_TLS_KEY_FILE` | *(empty)* | Serve HTTPS from your own certificate files |
| `TDOCS_HTTP_PORT` / `TDOCS_HTTPS_PORT` | `80` / `443` | Ports for built-in ACME HTTPS |
| `TDOCS_BACKUP_INTERVAL` | `24h` | Automatic snapshot interval |
| `TDOCS_NO_BROWSER` | *(empty)* | Set `1` to skip opening a browser |

Copy `.env.example` → config dir `.env` (mode `600`), or run `tdocs setup`.  
**Never commit `.env`, `*.db`, sessions, or keys.**

---

## 🔄 Upgrade

Upgrades never delete the database, Telegram session, shares, or settings.

When a newer release exists, the dashboard shows an **update banner** with the
exact upgrade command for your install type (checked against GitHub at most
once per day; dismissal is remembered per version). `tdocs update --check`
reports the same from the CLI.

**Debian / Ubuntu**

```bash
sudo apt install ./tdocs_<new>_amd64.deb
sudo systemctl restart tdocs
```

**RPM**

```bash
sudo dnf upgrade ./tdocs_<new>_x86_64.rpm   # or yum / zypper / rpm -U
sudo systemctl restart tdocs
```

**Tarball**

```bash
# replace binary, keep data
sudo systemctl stop tdocs
sudo install -m 755 ./tdocs-new /usr/bin/tdocs
sudo systemctl start tdocs
```

**Built-in helper** (tarball installs only, amd64/arm64 + checksum verification):

```bash
tdocs update           # download latest release, verify SHA256, swap binary
tdocs update --check   # report only
```

> `tdocs update` refuses on `.deb`/`.rpm` installs and prints the right
> package-manager command instead — in-place swaps would desync dpkg/rpm.
> There is no one-click in-place upgrade from the dashboard by design.

Apply schema migrations with `tdocs migrate` (also runs automatically on start). A database written by a **newer** tDocs fails safely with a clear error instead of corrupting data.

---

## 💾 Backup & restore

```bash
tdocs backup     # snapshot SQLite → Telegram Storage Channel
tdocs restore    # point-in-time restore from latest snapshot
```

Also available from the dashboard (Snapshot History). Offline safety copy:

```bash
sudo cp /var/lib/tdocs/tdocs.db /safe/tdocs.db
```

---

## 🖥️ CLI

```bash
tdocs                  # first-run / start (wizard if unconfigured)
tdocs version          # version, commit, build date, go, platform
tdocs setup            # configuration wizard
tdocs start            # setup → login → server + browser
tdocs server           # dashboard + API + CDN only
tdocs doctor           # config/db/session/port/permissions report
tdocs status           # paths, schema, HTTP, Telegram pairing
tdocs login [--reset]  # Telegram pairing (OTP + 2FA)
tdocs logout
tdocs passwd <new>     # dashboard password (Argon2id)
tdocs upload <file> [--folder <id>]
tdocs download <id> [--output <path>]
tdocs list
tdocs backup
tdocs restore
tdocs migrate          # safe schema migrations
tdocs service install|remove [--user]
tdocs update [--check]
tdocs uninstall [--purge]   # never deletes data without --purge + confirm
```

---

## 🎬 CDN + API

```bash
curl -H "Authorization: Bearer ADMIN_PASS" \
  "http://localhost:8080/api/cdn/files?mime=video/&limit=100"
```

```html
<video controls preload="metadata" src="http://localhost:8080/cdn/<file_id>/stream"></video>
```

| Endpoint | Auth | Purpose |
| :--- | :--- | :--- |
| `GET /api/cdn/files?search=&mime=video/&limit=` | Bearer/token/cookie | Catalog + `stream_url` |
| `GET /cdn/{id}/stream` | public* | `<video>/<audio>/<img>` (Range 206) |
| `POST /api/share` | cookie/Bearer | Share link (password, expiry, limit) |
| `GET /api/openapi.json`, `GET /docs` | — | OpenAPI 3.0 + Swagger UI |

\* `TDOCS_CDN_PUBLIC=false` locks `/cdn/*` behind the Bearer key.

Full list: [docs/USER_GUIDE.md](docs/USER_GUIDE.md)

---

## 🗑️ Uninstall

```bash
# package
sudo apt remove tdocs        # or dnf remove tdocs / zypper remove tdocs

# tarball / any install — keeps data unless --purge
tdocs uninstall
tdocs uninstall --purge      # asks before deleting config + database + session
```

**Data is never deleted silently.**

---

## 🛠️ Troubleshooting

| Symptom | Fix |
| :--- | :--- |
| `decrypt session: cipher: message authentication failed` | Secret changed → `tdocs login --reset`, restart |
| Uploads fail | Not paired → `tdocs login` |
| Empty file list after restore | Metadata rebuilds from the Storage Channel on start, or `POST /api/sync` |
| Port already in use | Server auto-shifts +1…+50, or set `TDOCS_PORT` |
| Service won't start | `sudo journalctl -u tdocs -e` · `tdocs doctor` |
| Forgot dashboard password | `sudo -u tdocs tdocs passwd 'new-password'` then restart the service |
| `unable to open database file` di `/var/lib/tdocs` | Hak akses: jalankan `sudo -u tdocs tdocs <perintah>` (atau `sudo tdocs <perintah>`), lalu restart service |
| Telegram wizard won't start ("not initialized" / "not configured") | Login sebagai admin dulu, lalu isi API_ID/HASH via `sudo -u tdocs tdocs setup` dan `sudo systemctl restart tdocs` |
| Check download integrity | Compare with `SHA256SUMS` on the release page |

More: [SECURITY.md](SECURITY.md) · [docs/USER_GUIDE.md](docs/USER_GUIDE.md)

---

## 🛠️ Development

Source builds, tests, and contribution workflow live in **[DEVELOPMENT.md](DEVELOPMENT.md)**.

```bash
make build && make test && make lint
```

---

## 📚 Architecture

- [ARCHITECTURE.md](ARCHITECTURE.md) — system design
- [SECURITY.md](SECURITY.md) — security model
- [CONTEXT.md](CONTEXT.md) — domain language
- [docs/adr/](docs/adr/) — architecture decision records

---

Dibuat oleh **Robby Aprianto** ([@robprian](https://github.com/robprian)) · [Source](https://github.com/robprian/tDocs) · [MIT](LICENSE)

> Upgrade dari RobDocs/TeleDrive: variabel `ROBDOCS_*`/`TELEDRIVE_*`, `robdocs.db`/`teledrive.db`, `.robdocs.key`/`.teledrive.key`, dan snapshot lama tetap terbaca otomatis.
