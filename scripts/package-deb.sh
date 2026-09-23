#!/usr/bin/env bash
# Build a Debian package: tdocs_<version>_<debarch>.deb
#
# Usage: scripts/package-deb.sh <version> <goarch> [bindir] [outdir]
#   goarch: amd64 | arm64 | 386 | arm … (mapped to Debian architectures)
#   bindir defaults to dist/linux/<arch>
#   outdir defaults to dist/deb/<debarch>
set -euo pipefail

VERSION_RAW="${1:?version required}"
GOARCH="${2:?goarch required}"
VERSION="${VERSION_RAW#v}"
BINDIR="${3:-}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# GOARCH → Debian architecture
case "$GOARCH" in
  amd64)  DEBARCH=amd64 ;;
  arm64)  DEBARCH=arm64 ;;
  386)    DEBARCH=i386 ;;
  arm)    DEBARCH=armhf ;;
  ppc64le) DEBARCH=ppc64el ;;
  s390x)  DEBARCH=s390x ;;
  *) echo "unsupported goarch for deb: $GOARCH" >&2; exit 1 ;;
esac

if [ -z "$BINDIR" ]; then
  case "$GOARCH" in
    arm) BINDIR="dist/linux/armv7" ;;
    *)   BINDIR="dist/linux/$GOARCH" ;;
  esac
fi
OUTDIR="${4:-dist/deb/$DEBARCH}"

BIN="$BINDIR/tdocs"
if [ ! -f "$BIN" ]; then
  echo "missing binary: $BIN" >&2
  exit 1
fi
command -v dpkg-deb >/dev/null || { echo "dpkg-deb not found" >&2; exit 1; }

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

PKG="$STAGE/pkg"
mkdir -p "$PKG/DEBIAN" \
         "$PKG/usr/bin" \
         "$PKG/usr/lib/systemd/system" \
         "$PKG/etc/tdocs" \
         "$PKG/usr/share/doc/tdocs"

install -m 755 "$BIN" "$PKG/usr/bin/tdocs"
install -m 644 packaging/systemd/tdocs.service "$PKG/usr/lib/systemd/system/tdocs.service"
install -m 644 LICENSE "$PKG/usr/share/doc/tdocs/copyright"
install -m 644 README.md "$PKG/usr/share/doc/tdocs/README.md"
install -m 644 .env.example "$PKG/usr/share/doc/tdocs/env.example"
# Ship an empty config dir marker (real .env created by `tdocs setup`).
cat > "$PKG/etc/tdocs/.gitkeep" <<'EOF'
EOF
chmod 755 "$PKG/etc/tdocs"

MAINTAINER="${TDOCS_MAINTAINER:-Robby Aprianto <robprian@users.noreply.github.com>}"
HOMEPAGE="https://github.com/robprian/tDocs"

# MTProto (Telegram) and `tdocs update` (GitHub API) need TLS trust anchors.
cat > "$PKG/DEBIAN/control" <<EOF
Package: tdocs
Version: $VERSION
Architecture: $DEBARCH
Maintainer: $MAINTAINER
Section: net
Priority: optional
Depends: ca-certificates
Homepage: $HOMEPAGE
Installed-Size: $(du -sk "$PKG" | cut -f1)
Description: Self-hosted personal cloud storage using Telegram MTProto
 tDocs bridges a web dashboard and virtual file system to Telegram's
 MTProto infrastructure as a high-capacity object store. Single static
 binary with embedded frontend — no Go, Node.js, or database server
 required at runtime.
 .
 Features: resumable uploads, HTTP 206 streaming, share links, trash,
 versions, automated SQLite snapshots, public CDN + OpenAPI.
EOF

# Conffiles: never clobber an existing production .env on upgrade.
if [ -f "$PKG/etc/tdocs/.env" ]; then
  echo "/etc/tdocs/.env" >> "$PKG/DEBIAN/conffiles"
fi

cat > "$PKG/DEBIAN/preinst" <<'EOF'
#!/bin/sh
set -e
# Create a dedicated service account for system-wide installs.
if ! getent passwd tdocs >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/tdocs --shell "$(command -v nologin || echo /usr/sbin/nologin)" --user-group tdocs 2>/dev/null || true
fi
mkdir -p /var/lib/tdocs /etc/tdocs
chown tdocs:tdocs /var/lib/tdocs 2>/dev/null || true
chmod 750 /var/lib/tdocs 2>/dev/null || true
chmod 750 /etc/tdocs 2>/dev/null || true
exit 0
EOF
chmod 755 "$PKG/DEBIAN/preinst"

cat > "$PKG/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
# Preserve an existing config; new installs start empty until `tdocs setup`.
if [ ! -f /etc/tdocs/.env ] && [ -f /usr/share/doc/tdocs/env.example ]; then
  install -m 600 /usr/share/doc/tdocs/env.example /etc/tdocs/.env 2>/dev/null || true
  chown root:tdocs /etc/tdocs/.env 2>/dev/null || true
fi
exit 0
EOF
chmod 755 "$PKG/DEBIAN/postinst"

cat > "$PKG/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if [ -d /run/systemd/system ]; then
  systemctl disable --now tdocs.service >/dev/null 2>&1 || true
fi
exit 0
EOF
chmod 755 "$PKG/DEBIAN/prerm"

cat > "$PKG/DEBIAN/postrm" <<'EOF'
#!/bin/sh
set -e
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
# Never delete /var/lib/tdocs or /etc/tdocs on purge from here —
# `tdocs uninstall --purge` is the explicit path for data removal.
exit 0
EOF
chmod 755 "$PKG/DEBIAN/postrm"

mkdir -p "$OUTDIR"
NAME="tdocs_${VERSION}_${DEBARCH}.deb"
dpkg-deb --build --root-owner-group "$PKG" "$OUTDIR/$NAME"
echo "==> $OUTDIR/$NAME"
