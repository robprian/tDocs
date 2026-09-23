#!/usr/bin/env bash
# Generate SHA256SUMS for every artifact under dist/ (excluding checksums).
#
# Usage: scripts/checksums.sh [dist-dir]
set -euo pipefail

DIST="${1:-dist}"
cd "$(dirname "$0")/.."

command -v sha256sum >/dev/null || { echo "sha256sum not found" >&2; exit 1; }

mkdir -p "$DIST/checksums"
OUT="$DIST/checksums/SHA256SUMS"
: > "$OUT"

# Collect release artifacts (tar/deb/rpm/zip) but not nested checksum files.
while IFS= read -r -d '' f; do
  rel="${f#"$DIST"/}"
  (cd "$DIST" && sha256sum "$rel") >> "$OUT"
done < <(find "$DIST" \( -name 'tdocs_*.tar.gz' -o -name 'tdocs_*.deb' -o -name 'tdocs_*.rpm' -o -name 'tdocs_*.zip' -o -name 'tdocs-*.tar.gz' -o -name 'tdocs-*.zip' \) ! -path '*/checksums/*' -print0 | sort -z)

# Also copy to dist root for release upload convenience.
cp "$OUT" "$DIST/SHA256SUMS"
echo "==> $DIST/SHA256SUMS ($(wc -l < "$OUT") files)"
cat "$OUT"
