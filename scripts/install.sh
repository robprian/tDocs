#!/usr/bin/env bash
# tDocs installer (universal tarball).
#
#   ./install.sh              system install when root, else user-local
#   sudo ./install.sh         system install (/usr/bin + systemd + service user)
#   ./install.sh --user       force user-local (~/.local/bin)
#   ./install.sh --prefix DIR install binary under DIR/bin
#   ./install.sh --no-service skip systemd unit installation
#   ./install.sh --dry-run    print actions without changing the system
#
# Never overwrites existing configuration or data without saying so.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DRY=0
FORCE_USER=0
NO_SERVICE=0
PREFIX=""
UNIT_OK=0

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1; shift ;;
    --user) FORCE_USER=1; shift ;;
    --no-service) NO_SERVICE=1; shift ;;
    --prefix) PREFIX="${2:?--prefix needs a path}"; shift 2 ;;
    -h|--help)
      sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

run() {
  if [ "$DRY" -eq 1 ]; then
    echo "  [dry-run] $*"
  else
    "$@"
  fi
}

# --- environment checks ----------------------------------------------------
case "$(uname -s)" in
  Linux) ;;
  *) echo "✗ This installer targets Linux. Detected: $(uname -s)" >&2; exit 1 ;;
esac

# Only linux/x86_64 ships release binaries: matches the release workflow and
# the upgrade hint logic, so users get a clear error instead of a 404.
ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64) ARCH=amd64 ;;
  *) echo "✗ Only x86_64 has published releases (detected: $ARCH_RAW)." >&2
     echo "  Build from source: git clone https://github.com/robprian/tDocs && make build" >&2
     exit 1 ;;
esac

BIN_SRC="$SCRIPT_DIR/tdocs"
if [ ! -f "$BIN_SRC" ]; then
  echo "✗ tdocs binary not found next to install.sh ($BIN_SRC)" >&2
  echo "  Extract the full tarball first: tar -xzf tdocs_*.tar.gz && cd tdocs && ./install.sh" >&2
  exit 1
fi
if ! "$BIN_SRC" version >/dev/null 2>&1; then
  echo "✗ Bundled binary failed to run (wrong architecture?)." >&2
  "$BIN_SRC" version || true
  exit 1
fi

# --- choose install mode ---------------------------------------------------
SYSTEM=0
if [ -n "$PREFIX" ]; then
  SYSTEM=0
elif [ "$FORCE_USER" -eq 1 ]; then
  SYSTEM=0
elif [ "$(id -u)" -eq 0 ]; then
  SYSTEM=1
fi

if [ "$SYSTEM" -eq 1 ]; then
  BIN_DIR="${PREFIX:-/usr/bin}"
  CONFIG_DIR="${TDOCS_CONFIG_DIR:-/etc/tdocs}"
  DATA_DIR="${TDOCS_DATA_DIR:-/var/lib/tdocs}"
  UNIT_PATH="/etc/systemd/system/tdocs.service"
  UNIT_SRC="$SCRIPT_DIR/systemd/tdocs.service"
  SERVICE_USER=tdocs
  MODE=system
else
  BIN_DIR="${PREFIX:-$HOME/.local/bin}"
  CONFIG_DIR="${TDOCS_CONFIG_DIR:-$HOME/.config/tdocs}"
  DATA_DIR="${TDOCS_DATA_DIR:-$HOME/.local/share/tdocs}"
  UNIT_PATH="$HOME/.config/systemd/user/tdocs.service"
  UNIT_SRC="$SCRIPT_DIR/systemd/tdocs.service"
  SERVICE_USER=""
  MODE=user
fi

echo "  ◈ tDocs installer"
echo "  Architecture : $ARCH ($ARCH_RAW)"
echo "  Mode         : $MODE"
echo "  Binary       : $BIN_DIR/tdocs"
echo "  Config       : $CONFIG_DIR"
echo "  Data         : $DATA_DIR"
if [ "$NO_SERVICE" -eq 0 ]; then
  echo "  Service unit : $UNIT_PATH"
else
  echo "  Service unit : skipped (--no-service)"
fi
echo ""

# --- install binary --------------------------------------------------------
run mkdir -p "$BIN_DIR"
if [ -f "$BIN_DIR/tdocs" ] && ! cmp -s "$BIN_SRC" "$BIN_DIR/tdocs"; then
  if [ "$DRY" -eq 0 ]; then
    cp -p "$BIN_DIR/tdocs" "$BIN_DIR/tdocs.bak" 2>/dev/null || true
    echo "  ○ Existing binary backed up to $BIN_DIR/tdocs.bak"
  else
    echo "  [dry-run] backup existing binary"
  fi
fi
run install -m 755 "$BIN_SRC" "$BIN_DIR/tdocs"

# --- directories -----------------------------------------------------------
run mkdir -p "$CONFIG_DIR" "$DATA_DIR"
if [ "$DRY" -eq 0 ]; then
  chmod 700 "$CONFIG_DIR" "$DATA_DIR" 2>/dev/null || true
fi

# Preserve existing config: never clobber .env.
if [ ! -f "$CONFIG_DIR/.env" ] && [ -f "$SCRIPT_DIR/systemd/tdocs.service" ]; then
  if [ -f "$SCRIPT_DIR/../.env.example" ] || [ -f /usr/share/doc/tdocs/env.example ]; then
    :
  fi
fi

# Seed config from packaged example if present (mode 600, no secrets).
ENV_EXAMPLE=""
for candidate in \
  "$SCRIPT_DIR/../.env.example" \
  "$SCRIPT_DIR/.env.example" \
  /usr/share/doc/tdocs/env.example; do
  if [ -f "$candidate" ]; then ENV_EXAMPLE="$candidate"; break; fi
done
if [ -n "$ENV_EXAMPLE" ] && [ ! -f "$CONFIG_DIR/.env" ]; then
  run cp "$ENV_EXAMPLE" "$CONFIG_DIR/.env"
  if [ "$DRY" -eq 0 ]; then chmod 600 "$CONFIG_DIR/.env"; fi
  echo "  ○ Seeded $CONFIG_DIR/.env from example (edit before first start)"
fi

# --- dedicated service user (system mode) ----------------------------------
if [ "$SYSTEM" -eq 1 ] && [ "$NO_SERVICE" -eq 0 ]; then
  if command -v getent >/dev/null && command -v useradd >/dev/null; then
    if ! getent passwd tdocs >/dev/null 2>&1; then
      run useradd --system --home-dir "$DATA_DIR" \
        --shell "$(command -v nologin || echo /usr/sbin/nologin)" \
        --user-group tdocs 2>/dev/null || \
      run useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin tdocs || true
      echo "  ○ Created system user 'tdocs'"
    fi
    if [ "$DRY" -eq 0 ]; then
      chown -R tdocs:tdocs "$DATA_DIR" 2>/dev/null || true
      chown root:tdocs "$CONFIG_DIR" 2>/dev/null || true
      chmod 750 "$DATA_DIR" "$CONFIG_DIR" 2>/dev/null || true
    else
      echo "  [dry-run] chown data/config to tdocs:tdocs"
    fi
  fi
fi

# --- systemd unit ----------------------------------------------------------
if [ "$NO_SERVICE" -eq 0 ] && command -v systemctl >/dev/null 2>&1 && [ -f "$UNIT_SRC" ]; then
  # Rewrite paths in the unit for non-default locations / user mode.
  if [ "$SYSTEM" -eq 1 ]; then
    UNIT_TMP="$(mktemp)"
    sed -e "s|/usr/bin/tdocs|$BIN_DIR/tdocs|g" \
        -e "s|/var/lib/tdocs|$DATA_DIR|g" \
        -e "s|/etc/tdocs|$CONFIG_DIR|g" \
        "$UNIT_SRC" > "$UNIT_TMP"
    if [ "$DRY" -eq 1 ]; then
      echo "  [dry-run] install system unit → $UNIT_PATH"
      rm -f "$UNIT_TMP"
    else
      if [ -w "$(dirname "$UNIT_PATH")" ]; then
        install -m 644 "$UNIT_TMP" "$UNIT_PATH"
      else
        sudo install -m 644 "$UNIT_TMP" "$UNIT_PATH"
      fi
      rm -f "$UNIT_TMP"
      sudo systemctl daemon-reload >/dev/null 2>&1 || systemctl daemon-reload >/dev/null 2>&1 || true
      echo "  ✓ Systemd unit installed: $UNIT_PATH"
      echo "      sudo systemctl enable --now tdocs"
      UNIT_OK=1
    fi
  else
    # User unit: rewrite ExecStart and WorkingDirectory, drop User=/hardening
    # that assumes system paths.
    UNIT_TMP="$(mktemp)"
    sed -e "s|/usr/bin/tdocs|$BIN_DIR/tdocs|g" \
        -e "s|/var/lib/tdocs|$DATA_DIR|g" \
        -e "s|/etc/tdocs|$CONFIG_DIR|g" \
        -e "/^User=/d" -e "/^Group=/d" \
        -e "/^ProtectSystem=/d" -e "/^ProtectHome=/d" \
        -e "/^ReadWritePaths=/d" \
        -e "s|WantedBy=multi-user.target|WantedBy=default.target|" \
        "$UNIT_SRC" > "$UNIT_TMP"
    run mkdir -p "$(dirname "$UNIT_PATH")"
    run install -m 644 "$UNIT_TMP" "$UNIT_PATH"
    rm -f "$UNIT_TMP"
    if [ "$DRY" -eq 0 ]; then
      systemctl --user daemon-reload >/dev/null 2>&1 || true
      echo "  ✓ User systemd unit installed: $UNIT_PATH"
      echo "      systemctl --user enable --now tdocs"
      UNIT_OK=1
    fi
  fi
elif [ "$NO_SERVICE" -eq 1 ]; then
  echo "  ○ Skipped systemd unit (--no-service)"
else
  echo "  ○ systemctl not found — binary installed without a service unit"
fi

# --- PATH hint -------------------------------------------------------------
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "  ○ Add to PATH:  export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

VERSION_LINE="$("$BIN_DIR/tdocs" version 2>/dev/null | grep -a -m1 -o 'tDocs v[0-9.]*' || echo 'tDocs')"

cat <<EOF

tDocs installed successfully.

Binary:
  $BIN_DIR/tdocs  (${VERSION_LINE})

Data:
  $DATA_DIR

Config:
  $CONFIG_DIR

Service:
  $( [ "$UNIT_OK" -eq 1 ] && echo "$UNIT_PATH" || echo 'not installed — run binary directly or reinstall without --no-service' )

Start:
  $( [ "$UNIT_OK" -eq 1 ] && { [ "$SYSTEM" -eq 1 ] && echo "sudo systemctl enable --now tdocs" || echo "systemctl --user enable --now tdocs"; } || echo "$BIN_DIR/tdocs start" )

First-time setup:
  $BIN_DIR/tdocs setup     # Telegram API_ID/HASH + dashboard password
  $BIN_DIR/tdocs login     # pair Telegram (OTP + 2FA)
  $BIN_DIR/tdocs doctor    # verify configuration

Dashboard:
  http://localhost:8080

Logs:
  $( [ "$UNIT_OK" -eq 1 ] && { [ "$SYSTEM" -eq 1 ] && echo "sudo journalctl -u tdocs -f" || echo "journalctl --user -u tdocs -f"; } || echo "$BIN_DIR/tdocs status" )
EOF
