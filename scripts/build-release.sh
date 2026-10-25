#!/usr/bin/env bash
# Build release binaries for every supported Linux (and optional other) target.
#
# Usage:
#   scripts/build-release.sh <version> [outdir]
#
# Environment:
#   TDOCS_ARCHES   space-separated GOARCH values (default: "amd64"; other
#                  arches build from source but are not published)
#   TDOCS_OS       GOOS (default: linux)
set -euo pipefail

VERSION="${1:-dev}"
OUT="${2:-dist}"
ARCH_ARG="${3:-}"
GOOS_TARGET="${TDOCS_OS:-linux}"
ARCHES="${TDOCS_ARCHES:-${ARCH_ARG:-amd64}}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PKG="./cmd/tdocs"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%d)"
LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X main.Version=${VERSION#v}"
LDFLAGS="$LDFLAGS -X main.Commit=${COMMIT}"
LDFLAGS="$LDFLAGS -X main.BuildDate=${BUILD_DATE}"

mkdir -p "$OUT"

# Map GOARCH → directory/arch alias used in artifact names.
# arm with GOARM=7 is published as armv7.
arch_dir() {
  case "$1" in
    arm) echo "armv7" ;;
    386) echo "386" ;;
    *) echo "$1" ;;
  esac
}

echo "==> Building tDocs ${VERSION} for ${GOOS_TARGET}: ${ARCHES}"

for arch in $ARCHES; do
  dir="$(arch_dir "$arch")"
  target_dir="$OUT/$GOOS_TARGET/$dir"
  mkdir -p "$target_dir"
  out="$target_dir/tdocs"
  echo "  • ${GOOS_TARGET}/${arch} → $out"

  extra_env=()
  if [ "$arch" = "arm" ]; then
    extra_env+=(GOARM=7)
  fi

  # Pure Go stack (modernc.org/sqlite + gotd): CGO stays off for static linking.
  env CGO_ENABLED=0 GOOS="$GOOS_TARGET" GOARCH="$arch" "${extra_env[@]}" \
    go build -trimpath -ldflags="$LDFLAGS" -o "$out" "$PKG"

  # Sanity: must be a file and executable.
  test -f "$out"
  chmod +x "$out"
done

echo "==> Binaries written under $OUT/$GOOS_TARGET/"
