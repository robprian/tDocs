#!/usr/bin/env bash
# tDocs uninstaller (universal tarball).
#
#   ./uninstall.sh              remove binary + service unit (keeps data)
#   ./uninstall.sh --purge      also remove config + data (asks for confirmation)
#   ./uninstall.sh --purge --yes  non-interactive purge (CI / scripted)
#   ./uninstall.sh --dry-run    print actions only
#
# User data (database, Telegram session, encryption key, config) is never
# deleted unless --purge is passed AND confirmed (or --yes is also given).
set -euo pipefail

DRY=0
PURGE=0
YES=0

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1; shift ;;
    --purge) PURGE=1; shift ;;
    --yes|-y) YES=1; shift ;;
    -h|--help)
      sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

run() {
  if [ "$DRY" -eq 1 ]; then echo "  [dry-run] $*"; else "$@"; fi
}

confirm() {
  local prompt="$1"
  if [ "$YES" -eq 1 ]; then return 0; fi
  printf '%s [y/N]: ' "$prompt"
  local ans
  read -r ans
  case "$ans" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

# Detect where tDocs lives.
BIN=""
for candidate in \
  "$(command -v tdocs 2>/dev/null || true)" \
  /usr/bin/tdocs \
  /usr/local/bin/tdocs \
  "$HOME/.local/bin/tdocs"; do
  if [ -n "$candidate" ] && [ -x "$candidate" ]; then
    BIN="$candidate"
    break
  fi
done

if [ -z "$BIN" ]; then
  # Fall back to sibling binary from the extracted tarball.
  SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
  if [ -x "$SCRIPT_DIR/tdocs" ]; then
    BIN="$SCRIPT_DIR/tdocs"
  else
    echo "✗ tdocs binary not found on PATH or next to uninstall.sh" >&2
    exit 1
  fi
fi

echo "  ◈ tDocs · uninstall"
echo "  Binary: $BIN"

# Resolve data/config from the binary when possible (non-interactive).
CONFIG_DIR="${TDOCS_CONFIG_DIR:-}"
DATA_DIR="${TDOCS_DATA_DIR:-}"
if [ -z "$CONFIG_DIR" ] || [ -z "$DATA_DIR" ]; then
  # Best-effort defaults matching install.sh / production paths.
  if [ "$(id -u)" -eq 0 ]; then
    CONFIG_DIR="${CONFIG_DIR:-/etc/tdocs}"
    DATA_DIR="${DATA_DIR:-/var/lib/tdocs}"
  else
    CONFIG_DIR="${CONFIG_DIR:-$HOME/.config/tdocs}"
    DATA_DIR="${DATA_DIR:-$HOME/.local/share/tdocs}"
  fi
fi
DB_PATH="${TDOCS_DB_PATH:-$DATA_DIR/tdocs.db}"

echo "  Config: $CONFIG_DIR"
echo "  Data:   $DATA_DIR"
echo "  DB:     $DB_PATH"
echo ""

# Stop and remove service units.
if command -v systemctl >/dev/null 2>&1; then
  if systemctl is-active --quiet tdocs.service 2>/dev/null; then
    echo "  ○ Stopping tdocs.service …"
    run systemctl stop tdocs.service || true
  fi
  run systemctl disable tdocs.service 2>/dev/null || true
fi
for unit in \
  /etc/systemd/system/tdocs.service \
  /usr/lib/systemd/system/tdocs.service \
  /lib/systemd/system/tdocs.service \
  "$HOME/.config/systemd/user/tdocs.service"; do
  if [ -f "$unit" ]; then
    if [ -w "$(dirname "$unit")" ]; then
      run rm -f "$unit"
    else
      run sudo rm -f "$unit"
    fi
    echo "  ✓ Removed unit $unit"
  fi
done
if command -v systemctl >/dev/null 2>&1; then
  run systemctl daemon-reload 2>/dev/null || true
  run systemctl --user daemon-reload 2>/dev/null || true
fi

# Remove binary.
if [ -f "$BIN" ]; then
  if confirm "Remove binary $BIN?"; then
    if [ -w "$(dirname "$BIN")" ]; then
      run rm -f "$BIN" "$BIN.bak"
    else
      run sudo rm -f "$BIN" "$BIN.bak"
    fi
    echo "  ✓ Binary removed"
  else
    echo "  ○ Binary kept"
  fi
fi

if [ "$PURGE" -eq 0 ]; then
  cat <<EOF

  ✓ Application files removed (if confirmed).
  ○ Configuration and data were KEPT:
      $CONFIG_DIR
      $DATA_DIR
    Re-run with --purge to delete them after confirmation.
EOF
  exit 0
fi

echo ""
echo "  ⚠ PURGE will permanently delete configuration and user data."
if confirm "Delete config $CONFIG_DIR ?"; then
  run rm -rf "$CONFIG_DIR"
  echo "  ✓ Config removed"
fi
if confirm "Delete data $DATA_DIR (database + Telegram session + keys) ?"; then
  run rm -rf "$DATA_DIR"
  if [ -f "$DB_PATH" ]; then
    run rm -f "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm"
  fi
  echo "  ✓ Data removed"
else
  echo "  ○ Data kept"
fi

echo "  ✓ Uninstall complete."
