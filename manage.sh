#!/usr/bin/env bash
#
# manage.sh — satu pintu manajemen proses tDocs.
#
#   ./manage.sh start [foreground]  jalankan server di background (log: tdocs.log)
#   ./manage.sh stop                hentikan server (graceful, tunggu snapshot shutdown)
#   ./manage.sh restart             stop + start
#   ./manage.sh status              proses, port, URL, kesehatan Telegram & jumlah file
#   ./manage.sh logs [-f]           tail tdocs.log
#   ./manage.sh update              rebuild binary dari source (+ restart bila sedang jalan)
#   ./manage.sh snapshot            backup DB sekarang → Storage Channel (via API)
#   ./manage.sh open                buka dashboard di browser
#   ./manage.sh doctor|backup|sync|list|login|...
#   ./manage.sh service-install     pasang systemd unit (auto-start saat boot)
#   ./manage.sh service-remove      lepas systemd unit
#   ./manage.sh help
#
# Semua path absolut terhadap lokasi skrip ini, jadi aman dipanggil dari mana saja.
# Variabel TDOCS_* di environment selalu menang atas nilai di .env.

set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$ROOT/tdocs"
PIDFILE="$ROOT/.tdocs.pid"
LOGFILE="$ROOT/tdocs.log"
LOCKDIR="$ROOT/.manage.lock"

# ---------------------------------------------------------------- helpers ---

# env_val KEY DEFAULT — TDOCS_ dulu, lalu ROBDOCS_ / TELEDRIVE_ legacy
# (environment maupun .env), terakhir default. KEY diminta tanpa prefix.
env_val() {
    local key="$1" def="$2" v="" k legacy
    for k in "TDOCS_${key}" "ROBDOCS_${key}" "TELEDRIVE_${key}"; do
        v="${!k:-}"
        if [ -z "$v" ] && [ -f "$ROOT/.env" ]; then
            v="$(sed -n "s/^${k}=//p" "$ROOT/.env" | tail -n1 \
                | sed -e "s/^[\"']//" -e "s/[\"']$//" -e 's/[[:space:]]*$//')"
        fi
        if [ -n "$v" ]; then
            printf '%s' "$v"
            return 0
        fi
    done
    printf '%s' "$def"
}

PORT="$(env_val PORT 8080)"
HOST="$(env_val HOST 0.0.0.0)"
DB_PATH="$(env_val DB_PATH tdocs.db)"
ADMIN_PASS="$(env_val ADMIN_PASSWORD admin123)"
case "$DB_PATH" in /*) ;; *) DB_PATH="$ROOT/$DB_PATH";; esac
# Cerminkan resolveDBPath di binary: bila default tdocs.db belum ada,
# tampilkan robdocs.db / teledrive.db warisan bila itulah yang dipakai server.
if [ "$(env_val DB_PATH "")" = "" ] && [ ! -f "$DB_PATH" ]; then
    for legacy in robdocs.db teledrive.db; do
        if [ -f "$ROOT/$legacy" ]; then
            DB_PATH="$ROOT/$legacy"
            break
        fi
    done
fi

need_bin() {
    if [ ! -x "$BIN" ]; then
        echo "  ✕ Binary $BIN tidak ada / tidak executable. Jalankan: ./manage.sh update" >&2
        return 1
    fi
}

# Lock anti double-start/stop/update bersamaan. Wajib dipasangkan dengan unlock.
acquire_lock() {
    if mkdir "$LOCKDIR" 2>/dev/null; then
        printf '%s' "$$" > "$LOCKDIR/pid"
        return 0
    fi
    # Lock ada: bila pemiliknya sudah mati, anggap stale lalu ambil alih.
    local owner=""
    owner="$(tr -dc '0-9' < "$LOCKDIR/pid" 2>/dev/null)"
    if [ -n "$owner" ] && ! kill -0 "$owner" 2>/dev/null; then
        rm -rf "$LOCKDIR"
        if mkdir "$LOCKDIR" 2>/dev/null; then
            printf '%s' "$$" > "$LOCKDIR/pid"
            return 0
        fi
    fi
    echo "  ✕ Operasi lain sedang berjalan (lock: $LOCKDIR). Coba lagi sebentar." >&2
    return 1
}

unlock() {
    rm -rf "$LOCKDIR"
}

# PID yang tercatat + masih proses tdocs yang hidup.
server_pid() {
    [ -f "$PIDFILE" ] || return 1
    local pid
    pid="$(tr -dc '0-9' < "$PIDFILE" 2>/dev/null)"
    [ -n "$pid" ] || return 1
    kill -0 "$pid" 2>/dev/null || return 1
    if [ -r "/proc/$pid/cmdline" ]; then
        tr '\0' ' ' < "/proc/$pid/cmdline" | grep -q "tdocs" || return 1
    fi
    printf '%s' "$pid"
}

# Port aktual server (server bisa geser +1..+50 bila port sibuk).
detect_port() {
    local p base="$PORT" i code
    for i in $(seq 0 50); do
        p=$((base + i))
        code="$(curl -s -o /dev/null -m 2 -w '%{http_code}' "http://127.0.0.1:${p}/login" 2>/dev/null)"
        if [ "$code" = "200" ] || [ "$code" = "303" ]; then
            printf '%s' "$p"
            return 0
        fi
    done
    return 1
}

# Login dashboard -> cookie jar (stdout = path jar). Gagal -> return 1.
# Argumen: $1 = port.
api_login() {
    local port="$1" jar code
    jar="$(mktemp)"
    code="$(curl -s -c "$jar" -o /dev/null -m 5 -w '%{http_code}' \
        --data-urlencode "password=$ADMIN_PASS" \
        "http://127.0.0.1:${port}/login" 2>/dev/null)"
    if [ "$code" = "303" ] || [ "$code" = "302" ]; then
        printf '%s' "$jar"
        return 0
    fi
    rm -f "$jar"
    return 1
}

# --------------------------------------------------------------- commands ---

cmd_start() {
    local foreground=0
    [ "${1:-}" = "foreground" ] && foreground=1

    local pid
    if pid="$(server_pid)"; then
        echo "  ○ Server sudah jalan (PID $pid). Lihat: ./manage.sh status"
        return 0
    fi
    rm -f "$PIDFILE"
    need_bin || return 1

    if [ "$foreground" -eq 1 ]; then
        echo "  ◈ tDocs foreground (Ctrl+C untuk berhenti)…"
        exec "$BIN" server
    fi

    acquire_lock || return 1
    # Cek ulang di dalam lock (balapan dua start bersamaan).
    if pid="$(server_pid)"; then
        unlock
        echo "  ○ Server sudah jalan (PID $pid). Lihat: ./manage.sh status"
        return 0
    fi

    echo "  ◈ Menyalakan tDocs (port $PORT, log: tdocs.log)…"
    {
        echo "===== $(date '+%F %T') manage.sh start ====="
    } >> "$LOGFILE"
    # NO_BROWSER: background run tidak boleh membuka browser.
    TDOCS_NO_BROWSER=1 setsid "$BIN" server >> "$LOGFILE" 2>&1 < /dev/null &
    printf '%s' "$!" > "$PIDFILE"
    disown 2>/dev/null || true

    local i live="" cur=""
    for i in $(seq 1 30); do
        sleep 1
        cur="$(tr -dc '0-9' < "$PIDFILE" 2>/dev/null)"
        if [ -z "$cur" ] || ! kill -0 "$cur" 2>/dev/null; then
            echo "  ✕ Proses mati saat startup. Cek: ./manage.sh logs"
            rm -f "$PIDFILE"
            unlock
            return 1
        fi
        if live="$(detect_port)"; then
            break
        fi
    done
    if [ -z "$live" ]; then
        echo "  ✕ Server tidak menjawab HTTP dalam 30 dtk. Cek: ./manage.sh logs"
        unlock
        return 1
    fi
    unlock
    echo "  ✓ Jalan (PID $(cat "$PIDFILE"), port $live) → http://localhost:${live}"
}

cmd_stop() {
    acquire_lock || return 1
    local pid
    if ! pid="$(server_pid)"; then
        echo "  ○ Server tidak jalan."
        rm -f "$PIDFILE"
        unlock
        return 0
    fi
    echo "  ◈ Menghentikan server (PID $pid, graceful — tulis snapshot dulu)…"
    kill -TERM "$pid" 2>/dev/null
    local i
    for i in $(seq 1 20); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 1
    done
    if kill -0 "$pid" 2>/dev/null; then
        echo "  ○ Masih hidup setelah 20 dtk → SIGKILL."
        kill -KILL "$pid" 2>/dev/null
        sleep 1
    fi
    rm -f "$PIDFILE"
    unlock
    echo "  ✓ Berhenti."
}

cmd_restart() {
    cmd_stop
    cmd_start "$@"
}

cmd_status() {
    local pid live="" rc=0
    echo "  ◈ tDocs · status"
    if pid="$(server_pid)"; then
        local uptime
        uptime="$(ps -o etime= -p "$pid" 2>/dev/null | tr -d ' ')"
        echo "  ✓ run: PID $pid (uptime ${uptime:-?})"
    else
        echo "  ✕ run: berhenti"
        rc=1
    fi

    if live="$(detect_port)"; then
        echo "  ✓ http: http://localhost:${live} (config port $PORT)"
    else
        echo "  ✕ http: tidak menjawab di port $PORT"
        rc=1
    fi
    if [ -f "$DB_PATH" ]; then
        echo "  ● db: $DB_PATH ($(du -h "$DB_PATH" | cut -f1))"
    else
        echo "  ○ db: $DB_PATH (belum ada)"
    fi

    if [ -n "$live" ] && jar="$(api_login "$live")"; then
        local st
        st="$(curl -s -b "$jar" -m 5 "http://127.0.0.1:${live}/api/status" 2>/dev/null)"
        rm -f "$jar"
        if command -v jq >/dev/null 2>&1; then
            echo "  ● telegram_session: $(echo "$st" | jq -r .telegram_session)  storage_channel: $(echo "$st" | jq -r .storage_channel)"
            echo "  ● files: $(echo "$st" | jq -r .files)  folders: $(echo "$st" | jq -r .folders)"
        else
            echo "  ● $st"
        fi
    elif [ -n "$live" ]; then
        echo "  ○ api: password admin tidak cocok (cek TDOCS_ADMIN_PASSWORD)"
    fi
    return "$rc"
}

cmd_logs() {
    [ -f "$LOGFILE" ] || { echo "  ○ Belum ada log ($LOGFILE)."; return 0; }
    if [ "${1:-}" = "-f" ] || [ "${1:-}" = "--follow" ]; then
        tail -n 100 -f "$LOGFILE"
    else
        tail -n "${1:-100}" "$LOGFILE"
    fi
}

cmd_update() {
    acquire_lock || return 1
    local was_running=0
    server_pid >/dev/null && was_running=1
    if [ "$was_running" -eq 1 ]; then
        unlock
        cmd_stop || { echo "  ✕ Gagal menghentikan server — update dibatalkan."; return 1; }
        acquire_lock || return 1
    fi

    # git pull hanya bila repo ini git (abaikan bila bukan).
    if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
        echo "  ◈ git pull --ff-only…"
        git -C "$ROOT" pull --ff-only || echo "  ○ git pull gagal — lanjut build dari source lokal."
    fi
    echo "  ◈ Build binary…"
    if ! (cd "$ROOT" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/tdocs); then
        echo "  ✕ Build gagal."
        unlock
        [ "$was_running" -eq 1 ] && echo "  ○ Server sebelumnya jalan — nyalakan manual: ./manage.sh start"
        return 1
    fi
    # Banner mengandung ANSI/box-drawing → ambil baris deskripsi versi yang bersih.
    echo "  ✓ Binary terbaru: $("$BIN" version 2>/dev/null | grep -a -m1 -o 'tDocs v[0-9.]*')"
    unlock
    if [ "$was_running" -eq 1 ]; then
        cmd_start
    else
        echo "  ○ Server sebelumnya berhenti — tidak auto-start. Jalankan: ./manage.sh start"
    fi
}

cmd_sync() {
    local live jar out
    live="$(detect_port)" || { echo "  ✕ Server tidak jalan. Jalankan: ./manage.sh start"; return 1; }
    jar="$(api_login "$live")" || { echo "  ✕ Login API gagal (cek TDOCS_ADMIN_PASSWORD)."; return 1; }
    out="$(curl -s -b "$jar" -m 60 -X POST "http://127.0.0.1:${live}/api/sync" 2>/dev/null)"
    rm -f "$jar"
    if command -v jq >/dev/null 2>&1 && echo "$out" | jq -e '.scanned' >/dev/null 2>&1; then
        echo "$out" | jq -r '"  ✓ sync: \(.scanned) scanned, \(.inserted) recovered, \(.updated) refreshed (total \(.total))"'
        return 0
    fi
    echo "  ✕ Sync gagal: $out"
    return 1
}

# snapshot = backup DB sekarang lewat API server yang sedang jalan.
cmd_snapshot() {
    local live jar out code
    live="$(detect_port)" || { echo "  ✕ Server tidak jalan. Jalankan: ./manage.sh start"; return 1; }
    jar="$(api_login "$live")" || { echo "  ✕ Login API gagal (cek TDOCS_ADMIN_PASSWORD)."; return 1; }
    local body
    body="$(mktemp)"
    code="$(curl -s -b "$jar" -m 120 -X POST -o "$body" -w '%{http_code}' "http://127.0.0.1:${live}/api/snapshots" 2>/dev/null)"
    rm -f "$jar"
    out="$(cat "$body")"
    rm -f "$body"
    if [ "$code" = "201" ]; then
        if command -v jq >/dev/null 2>&1 && echo "$out" | jq -e . >/dev/null 2>&1; then
            echo "$out" | jq -r '"  ✓ Snapshot tersimpan di Storage Channel (message \(.message_id))."'
        else
            echo "  ✓ Snapshot tersimpan: $out"
        fi
        return 0
    fi
    echo "  ✕ Snapshot gagal (HTTP $code): $out"
    return 1
}

# open = buka dashboard di browser default (sadar WSL/SSH/headless).
cmd_open() {
    local live
    live="$(detect_port)" || { echo "  ✕ Server tidak jalan. Jalankan: ./manage.sh start"; return 1; }
    local url="http://localhost:${live}"
    echo "  ◈ Membuka $url …"
    if [ -n "${SSH_CONNECTION:-}${SSH_TTY:-}" ]; then
        echo "  ○ Terdeteksi SSH — buka manual: $url"
        return 0
    fi
    if grep -qi microsoft /proc/version 2>/dev/null && command -v wslview >/dev/null 2>&1; then
        wslview "$url" && return 0
    fi
    case "$(uname -s)" in
        Darwin) open "$url" && return 0 ;;
        Linux) command -v xdg-open >/dev/null 2>&1 && xdg-open "$url" && return 0 ;;
    esac
    echo "  ○ Tidak ada pembuka browser — buka manual: $url"
}

cmd_passthrough() {
    need_bin || return 1
    exec "$BIN" "$@"
}

cmd_service_install() {
    command -v systemctl >/dev/null 2>&1 || { echo "  ✕ systemctl tidak tersedia di host ini."; return 1; }
    local unit="tdocs.service" dest="/etc/systemd/system/$unit" mode="system"
    if [ "${1:-}" = "--user" ]; then
        mode="user"
        dest="$HOME/.config/systemd/user/$unit"
        mkdir -p "$(dirname "$dest")"
    fi
    cat > "/tmp/$unit" <<EOF
[Unit]
Description=tDocs — Telegram MTProto personal cloud
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$ROOT
ExecStart=$BIN server
Environment=TDOCS_NO_BROWSER=1
EnvironmentFile=-$ROOT/.env
Restart=always
RestartSec=5

[Install]
WantedBy=$([ "$mode" = "user" ] && echo "default.target" || echo "multi-user.target")
EOF
    if [ "$mode" = "user" ]; then
        mv "/tmp/$unit" "$dest"
        systemctl --user daemon-reload && systemctl --user enable --now "$unit"
        echo "  ✓ Service user terpasang & jalan: systemctl --user status $unit"
    else
        sudo mv "/tmp/$unit" "$dest"
        sudo systemctl daemon-reload && sudo systemctl enable --now "$unit"
        echo "  ✓ Service terpasang & jalan: sudo systemctl status $unit"
    fi
}

cmd_service_remove() {
    command -v systemctl >/dev/null 2>&1 || { echo "  ✕ systemctl tidak tersedia di host ini."; return 1; }
    if [ "${1:-}" = "--user" ]; then
        systemctl --user disable --now tdocs.service 2>/dev/null
        rm -f "$HOME/.config/systemd/user/tdocs.service"
        systemctl --user daemon-reload
    else
        sudo systemctl disable --now tdocs.service 2>/dev/null
        sudo rm -f /etc/systemd/system/tdocs.service
        sudo systemctl daemon-reload
    fi
    echo "  ✓ Service dilepas."
}

cmd_help() {
    sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//' | sed '/./,$!d'
    echo ""
    echo "  Contoh:"
    echo "    ./manage.sh start && ./manage.sh status"
    echo "    ./manage.sh logs -f"
    echo "    ./manage.sh sync            # pulihkan katalog dari channel Telegram"
    echo "    ./manage.sh snapshot        # backup DB sekarang → Storage Channel"
    echo "    ./manage.sh update          # rebuild + restart bila sedang jalan"
}

# ------------------------------------------------------------------ main ---

case "${1:-help}" in
    start)            shift; cmd_start "$@" ;;
    stop)             cmd_stop ;;
    restart)          shift; cmd_restart "$@" ;;
    status)           cmd_status ;;
    logs|log)         shift; cmd_logs "$@" ;;
    update|upgrade)   cmd_update ;;
    sync)             cmd_sync ;;
    snapshot)         cmd_snapshot ;;
    open)             cmd_open ;;
    service-install)  shift; cmd_service_install "$@" ;;
    service-remove)   shift; cmd_service_remove "$@" ;;
    clean)            rm -f "$PIDFILE"; : > "$LOGFILE" 2>/dev/null || true; echo "  ✓ PID & log dibersihkan." ;;
    version|--version|-v) cmd_passthrough version ;;
    help|--help|-h)   cmd_help ;;
    doctor|backup|restore|login|logout|setup|list|upload|download|server)
        cmd_passthrough "$@" ;;
    *) echo "  ✕ Perintah tidak dikenal: $1"; cmd_help; exit 1 ;;
esac
