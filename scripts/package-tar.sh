#!/usr/bin/env bash
# Assemble the universal Linux tarball:
#   tdocs_<version>_linux_<arch>.tar.gz
#
# Layout:
#   tdocs/
#   ├── tdocs
#   ├── README.md
#   ├── LICENSE
#   ├── install.sh
#   ├── uninstall.sh
#   └── systemd/tdocs.service
#
# Usage: scripts/package-tar.sh <version> <arch> [bindir] [outdir]
#   bindir defaults to dist/linux/<arch>
#   outdir defaults to dist/tar
set -euo pipefail

VERSION="${1:?version required}"
ARCH="${2:?arch required}"
BINDIR="${3:-dist/linux/$ARCH}"
OUTDIR="${4:-dist/tar}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BIN="$BINDIR/tdocs"
if [ ! -f "$BIN" ]; then
  echo "missing binary: $BIN (run scripts/build-release.sh first)" >&2
  exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE" 2>/dev/null || sudo rm -rf "$STAGE" 2>/dev/null || true' EXIT

PKGROOT="$STAGE/tdocs"
mkdir -p "$PKGROOT/systemd"

cp "$BIN" "$PKGROOT/tdocs"
chmod 755 "$PKGROOT/tdocs"
cp README.md LICENSE "$PKGROOT/"
cp scripts/install.sh scripts/uninstall.sh "$PKGROOT/"
chmod 755 "$PKGROOT/install.sh" "$PKGROOT/uninstall.sh"
cp packaging/systemd/tdocs.service "$PKGROOT/systemd/"

# Strip any accidental development artifacts (belt and braces).
rm -rf "$PKGROOT/.git" "$PKGROOT/node_modules" "$PKGROOT/go.mod" 2>/dev/null || true
find "$PKGROOT" -name '*.db' -o -name '.env' -o -name '*.key' -o -name '*.log' | xargs -r rm -f

mkdir -p "$OUTDIR"
NAME="tdocs_${VERSION#v}_linux_${ARCH}.tar.gz"
tar -C "$STAGE" -czf "$OUTDIR/$NAME" tdocs
echo "==> $OUTDIR/$NAME"
