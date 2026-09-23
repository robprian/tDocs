package main

import (
	"fmt"
	"os"

	"tdocs/internal/app"
	"tdocs/internal/db"
)

const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiCyan  = "\033[36m"
	ansiBlue  = "\033[34m"
	ansiGreen = "\033[32m"
	ansiAmber = "\033[33m"
)

// banner is rebuilt at runtime so release builds show the injected version.
func bannerText() string {
	return ansiCyan + ansiBold + fmt.Sprintf(`
  ╭──────────────────────────────────────────────────╮
  │   ◈  tDocs %-8s                             │
  │   Soft-UI cloud storage · Telegram MTProto      │
  │   by Robby Aprianto · MIT · github.com/robprian/tDocs │
  ╰──────────────────────────────────────────────────╯`, displayVersion()) + ansiReset + "\n"
}

// banner keeps the package-level name used by other files.
var banner = bannerText()

func main() {
	cfg := app.LoadConfig()

	// First-run / default entry: bare `tdocs` behaves like `tdocs start`
	// (setup wizard when unconfigured, otherwise the normal start flow).
	if len(os.Args) < 2 {
		runStart(cfg)
		return
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		printVersion()
	case "setup":
		runSetup(cfg)
	case "start":
		runStart(cfg)
	case "doctor":
		runDoctor(cfg)
	case "status":
		runStatus(cfg)
	case "login":
		runLogin(cfg)
	case "logout":
		runLogout(cfg)
	case "passwd", "password":
		runPasswd(cfg, os.Args[2:])
	case "server":
		runServer(cfg)
	case "upload":
		runUpload(cfg, os.Args[2:])
	case "download":
		runDownload(cfg, os.Args[2:])
	case "list":
		runList(cfg, os.Args[2:])
	case "backup":
		runBackup(cfg)
	case "restore":
		runRestore(cfg)
	case "migrate":
		runMigrate(cfg)
	case "service":
		runService(cfg, os.Args[2:])
	case "update":
		runUpdate(cfg, os.Args[2:])
	case "uninstall":
		runUninstall(cfg, os.Args[2:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("  Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(bannerText())
	fmt.Print(`  Usage:  tdocs <command> [arguments]

  ── First run (cukup sekali) ─────────────────────
    (no command)       Same as start: wizard bila belum terkonfigurasi
    setup              Wizard: API_ID/HASH + password + port → tulis config
    login              Pairing Telegram (OTP + 2FA), setelah setup
    start              Jalan pintas: setup bila perlu → buka browser → server
    doctor             Cek config/db/session/port + saran perbaikan
    status             Ringkasan path, versi, dan status server

  ── Account ──────────────────────────────────────
    login [--reset]    Pair Telegram via MTProto wizard (OTP + 2FA)
    logout             Clear stored Telegram session
    passwd <new>       Set dashboard password (Argon2id); --clear falls back
                       to TDOCS_ADMIN_PASSWORD

  ── Serve ────────────────────────────────────────
    server             Start neumorphic dashboard + API + CDN server

  ── Files ────────────────────────────────────────
    upload <file> [--folder <id>]
                       Upload to Storage Channel (512 KB parts, Safe Mode)
    download <file_id> [--output <path>]
                       Download a file by id
    list               List virtual folders & files

  ── Safety & ops ────────────────────────────────
    backup             Snapshot SQLite → Telegram Storage Channel now
    restore            Point-in-time restore from latest snapshot
    migrate            Apply safe database schema migrations
    service install    Install systemd unit (system or --user)
    service remove     Remove systemd unit
    update             Download & install the latest release (checksum-verified)
    uninstall          Remove application files (asks before deleting data)
    version            Show version, commit, build date, platform

  ── API (when server runs) ───────────────────────
    Dashboard   http://localhost:8080/  (cookie login)
    Index       GET /api                (Bearer or cookie → endpoint catalog)
    Contract    GET /api/openapi.json   (OpenAPI 3.0) · GET /docs (Swagger UI)
    Catalog     GET /api/cdn/files?mime=video/&limit=100
    Embed       GET /cdn/{id}/stream    (<video>/<audio>/<img>, Range 206)
    Auth        Authorization: Bearer <TDOCS_ADMIN_PASSWORD> or ?api_key=
`)
}

func openDatabase(cfg *app.Config) *db.DB {
	if cfg.Paths != nil && cfg.Paths.DataDir != "" && cfg.Paths.DataDir != "." {
		_ = cfg.Paths.EnsureDataDir()
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Error opening database at %s: %v\n", cfg.DBPath, err)
		os.Exit(1)
	}
	return database
}
