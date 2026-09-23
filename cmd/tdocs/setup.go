package main

// Setup wizard, one-shot starter, and diagnostics for frictionless runs:
//
//	tdocs setup   interactive .env wizard (API_ID/HASH + password + port)
//	tdocs start   setup when needed → optional login → open browser → server
//	tdocs doctor  read-only checks with fix hints
//
// The wizard only touches configuration (.env file + app_id/hash settings).
// It never deletes the Telegram session.

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"tdocs/internal/app"
	"tdocs/internal/db"
)

// openDBReadOnly opens the database without exiting the process, for
// diagnostics that must report errors instead of dying. Ensures the data
// directory exists so a fresh install diagnoses cleanly.
func openDBReadOnly(cfg *app.Config) (*db.DB, error) {
	if cfg.Paths != nil && cfg.Paths.DataDir != "" && cfg.Paths.DataDir != "." {
		_ = cfg.Paths.EnsureDataDir()
	}
	return db.Open(cfg.DBPath)
}

// promptLine prints "label [def]: " and returns trimmed input or def when empty.
func promptLine(reader *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("  %s [%s]: ", label, def)
	} else {
		fmt.Printf("  %s: ", label)
	}
	val, _ := reader.ReadString('\n')
	val = strings.TrimSpace(val)
	if val == "" {
		return def
	}
	return val
}

func storedSetting(database interface {
	GetSetting(string) (string, error)
}, key string) string {
	v, err := database.GetSetting(key)
	if err != nil {
		return ""
	}
	return v
}

// credentialsConfigured reports whether API_ID/HASH resolve from env or DB.
func credentialsConfigured(cfg *app.Config) bool {
	if cfg.TelegramAppID != 0 && cfg.TelegramAppHash != "" {
		return true
	}
	database, err := openDBReadOnly(cfg)
	if err != nil {
		return false
	}
	defer database.Close()
	id, _ := database.GetSetting("telegram_app_id")
	hash, _ := database.GetSetting("telegram_app_hash")
	return id != "" && id != "0" && hash != ""
}

// hasSession reports whether Telegram completed an interactive login.
// The marker (not raw session bytes) is authoritative: gotd persists session
// state even for runs that never authorized.
func hasSession(cfg *app.Config) bool {
	database, err := openDBReadOnly(cfg)
	if err != nil {
		return false
	}
	defer database.Close()
	_, err = database.GetSetting("telegram_authorized")
	return err == nil
}

func runSetup(cfg *app.Config) *app.Config {
	fmt.Println("  ◈ tDocs · Setup wizard")
	fmt.Println("  ─────────────────────────")
	fmt.Println("  Ambil API_ID + API_HASH sekali saja dari https://my.telegram.org → API development tools.")
	fmt.Println("")

	reader := bufio.NewReader(os.Stdin)
	database := openDatabase(cfg)
	defer database.Close()

	curID := strconv.Itoa(cfg.TelegramAppID)
	if curID == "0" {
		curID = storedSetting(database, "telegram_app_id")
	}
	curHash := cfg.TelegramAppHash
	if curHash == "" {
		curHash = storedSetting(database, "telegram_app_hash")
	}
	curPass := cfg.AdminPassword
	curPort := cfg.Port
	if curPort == "" {
		curPort = "8080"
	}

	idStr := promptLine(reader, "Telegram App ID", curID)
	appID, _ := strconv.Atoi(strings.TrimSpace(idStr))
	appHash := promptLine(reader, "Telegram App Hash", curHash)
	adminPass := promptLine(reader, "Password dashboard (admin)", curPass)
	port := promptLine(reader, "Port dashboard", curPort)

	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		fmt.Println("  ✕ Port tidak valid, pakai 8080.")
		port = "8080"
	}
	if strings.TrimSpace(adminPass) == "" {
		adminPass = "admin123"
	}

	// Write to the production config file when running outside a source
	// checkout; legacy .env in the working directory keeps winning when both
	// exist (loaded first by LoadConfig).
	envPath := ".env"
	if cfg.Paths != nil && cfg.Paths.Mode != "dev" {
		if err := cfg.Paths.EnsureConfigDir(); err == nil {
			envPath = cfg.Paths.EnvFile
		}
	}
	if err := upsertEnvFile(envPath, map[string]string{
		"TDOCS_TG_APP_ID":      strconv.Itoa(appID),
		"TDOCS_TG_APP_HASH":    appHash,
		"TDOCS_ADMIN_PASSWORD": adminPass,
		"TDOCS_PORT":           port,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Gagal menulis %s: %v\n", envPath, err)
		return cfg
	}
	if appID != 0 {
		_ = database.SetSetting("telegram_app_id", strconv.Itoa(appID))
	}
	if strings.TrimSpace(appHash) != "" {
		_ = database.SetSetting("telegram_app_hash", strings.TrimSpace(appHash))
	}

	fresh := app.LoadConfig()

	fmt.Println("")
	fmt.Printf("  ✓ Konfigurasi tersimpan di %s (0600).\n", envPath)
	if appID == 0 || strings.TrimSpace(appHash) == "" {
		fmt.Println("  ○ API_ID/HASH masih kosong — isi lagi via `tdocs setup` kapan saja.")
	} else {
		fmt.Println("  Langkah berikutnya:")
		fmt.Println("    1. tdocs login    # pairing Telegram (OTP + 2FA)")
		fmt.Println("    2. tdocs start    # buka dashboard otomatis")
	}
	return fresh
}

func runStart(cfg *app.Config) {
	fmt.Print(bannerText())
	fmt.Println("  ◈ tDocs · start")
	fmt.Println("  ──────────────────")

	// Step 1 — credentials (API_ID/API_HASH). Needed before any Telegram call.
	if !credentialsConfigured(cfg) {
		if !isInteractive() {
			fmt.Println("  ✕ Kredensial Telegram belum ada. Isi .env / environment, lalu mulai lagi.")
			fmt.Println("  Lokal: jalankan `tdocs setup` sekali saja.")
			return
		}
		fmt.Println("  ○ Kredensial Telegram belum ada — masuk ke setup dulu.")
		fmt.Println("")
		cfg = runSetup(cfg)
		fmt.Println("")
	}
	if !credentialsConfigured(cfg) {
		fmt.Println("  ✕ API_ID/HASH masih kosong. Jalankan `tdocs setup`, lalu `tdocs start` lagi.")
		return
	}
	fmt.Println("  ✓ Kredensial Telegram OK")

	// Step 2 — Telegram session. If absent, run the login wizard now so the
	// server never boots into a state where every upload silently fails.
	if !hasSession(cfg) {
		fmt.Println("  ○ Belum ada sesi Telegram tersimpan.")
		if !isInteractive() {
			fmt.Println("  ○ Mode non-interaktif: dashboard tetap jalan, upload aktif setelah `tdocs login`.")
		} else {
			reader := bufio.NewReader(os.Stdin)
			ans := promptLine(reader, "Jalankan wizard login Telegram sekarang? [Y/n]", "Y")
			if !strings.EqualFold(strings.TrimSpace(ans), "n") {
				fmt.Println("")
				runLogin(cfg)
				fmt.Println("")
				if !hasSession(cfg) {
					fmt.Println("  ✕ Login belum berhasil. Jalankan ulang `tdocs start` setelah login sukses.")
					return
				}
			} else {
				fmt.Println("  ○ Login dilewati — dashboard jalan, upload nonaktif sampai `tdocs login`.")
			}
		}
	} else {
		fmt.Println("  ✓ Sesi Telegram tersimpan")
	}

	// Step 3 — storage channel + banner + serve.
	fmt.Println("  ✓ Siap — menyalakan server & sinkronisasi channel...")
	fmt.Println("")
	runServer(cfg)
}

// isInteractive reports whether stdin is a real terminal.
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func runDoctor(cfg *app.Config) {
	fmt.Println("  ◈ tDocs · Doctor")
	fmt.Println("  ───────────────────")
	fails := 0
	warns := 0
	ok := func(msg string) { fmt.Printf("  ✓ %s\n", msg) }
	bad := func(msg, hint string) {
		fails++
		fmt.Printf("  ✕ %s\n    → %s\n", msg, hint)
	}
	warn := func(msg, hint string) {
		warns++
		fmt.Printf("  ○ %s\n    → %s\n", msg, hint)
	}

	// 0. Application / paths (never print secrets)
	fmt.Printf("  · Application: tDocs %s (%s/%s)\n", displayVersion(), runtime.GOOS, runtime.GOARCH)
	if cfg.Paths != nil {
		fmt.Printf("  · Mode:        %s\n", cfg.Paths.Mode)
		fmt.Printf("  · Config:      %s\n", cfg.Paths.ConfigDir)
		fmt.Printf("  · Data:        %s\n", cfg.Paths.DataDir)
	}
	fmt.Printf("  · Database:    %s\n", cfg.DBPath)

	// 1. config file(s)
	envFound := false
	if _, err := os.Stat(".env"); err == nil {
		ok(".env ditemukan di working directory")
		envFound = true
	}
	if cfg.Paths != nil && cfg.Paths.EnvFile != ".env" {
		if _, err := os.Stat(cfg.Paths.EnvFile); err == nil {
			ok(fmt.Sprintf("config file ditemukan: %s", cfg.Paths.EnvFile))
			envFound = true
		}
	}
	if !envFound {
		if os.Getenv("TDOCS_TG_APP_ID") != "" || os.Getenv("app_api_id") != "" {
			ok("kredensial dari environment (tanpa file config — OK)")
		} else {
			warn("belum ada file config / env kredensial", "jalankan `tdocs setup`")
		}
	}

	// 2. credentials + database + schema
	database, err := openDBReadOnly(cfg)
	if err != nil {
		bad(fmt.Sprintf("database %s tidak bisa dibuka: %v", cfg.DBPath, err), "cek permission folder / disk penuh")
	} else {
		defer database.Close()
		ok(fmt.Sprintf("database terbuka (%s)", cfg.DBPath))
		if ver, err := database.SchemaVersion(); err == nil {
			ok(fmt.Sprintf("database schema v%d (supported v%d)", ver, db.SchemaVersion))
		} else {
			warn("database schema version tidak terbaca", "jalankan `tdocs migrate`")
		}
		id := cfg.TelegramAppID
		hash := cfg.TelegramAppHash
		if id == 0 {
			if s := storedSetting(database, "telegram_app_id"); s != "" {
				id, _ = strconv.Atoi(s)
			}
		}
		if hash == "" {
			hash = storedSetting(database, "telegram_app_hash")
		}
		if id != 0 && hash != "" {
			ok("API_ID + API_HASH terkonfigurasi")
		} else {
			warn("API_ID/HASH belum lengkap", "jalankan `tdocs setup` (butuh dari my.telegram.org)")
		}
		if _, err := database.GetSetting("telegram_session"); err == nil {
			ok("sesi Telegram tersimpan")
		} else {
			warn("belum ada sesi Telegram", "jalankan `tdocs login` (butuh OTP + 2FA bila ada)")
		}
		if _, err := database.GetSetting("telegram_authorized"); err == nil {
			ok("login Telegram terverifikasi")
		} else {
			warn("login Telegram belum terverifikasi", "jalankan `tdocs login`, atau restart server agar verifikasi otomatis jalan")
		}

		// Storage permissions (data dir must not be world-readable).
		if cfg.Paths != nil && cfg.Paths.DataDir != "" && cfg.Paths.DataDir != "." {
			if fi, err := os.Stat(cfg.Paths.DataDir); err == nil {
				if perm := fi.Mode().Perm(); perm&0o077 != 0 {
					warn(fmt.Sprintf("data dir permissions %#o (group/other can read)", perm),
						fmt.Sprintf("chmod 700 %s", cfg.Paths.DataDir))
				} else {
					ok(fmt.Sprintf("data dir permissions %#o", perm))
				}
			}
		}
		if fi, err := os.Stat(cfg.DBPath); err == nil {
			if perm := fi.Mode().Perm(); perm&0o077 != 0 {
				warn(fmt.Sprintf("database permissions %#o", perm),
					fmt.Sprintf("chmod 600 %s", cfg.DBPath))
			}
		}
	}

	// 3. port
	port := cfg.Port
	if port == "" {
		port = "8080"
	}
	if ln, err := net.Listen("tcp", "127.0.0.1:"+port); err != nil {
		warn(fmt.Sprintf("port %s sedang dipakai", port), "server otomatis pindah port — atau kosongkan via TDOCS_PORT")
	} else {
		_ = ln.Close()
		ok(fmt.Sprintf("port %s bebas", port))
	}

	// 4. HTTP server / systemd (best-effort, no secrets)
	if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/api/health", port)); err == nil {
		_ = resp.Body.Close()
		ok(fmt.Sprintf("HTTP server menjawab di port %s", port))
	} else {
		warn("HTTP server tidak terdeteksi di port "+port, "jalankan `tdocs server` / `systemctl start tdocs`")
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		if out, err := exec.Command("systemctl", "is-enabled", "tdocs.service").Output(); err == nil {
			ok("systemd unit: " + strings.TrimSpace(string(out)))
		} else {
			warn("systemd unit tdocs.service tidak aktif", "tdocs service install && sudo systemctl enable --now tdocs")
		}
	}

	if cfg.AdminPassword == "admin123" {
		warn("masih pakai password default admin123", "ganti saat `tdocs setup` / TDOCS_ADMIN_PASSWORD")
	} else {
		ok("admin password sudah diganti")
	}

	fmt.Println("")
	if fails > 0 {
		fmt.Printf("  ✕ doctor: %d masalah, %d peringatan — ikuti saran → di atas.\n", fails, warns)
		os.Exit(1)
	}
	if warns > 0 {
		fmt.Printf("  ○ doctor: sehat, %d peringatan ringan.\n", warns)
		return
	}
	fmt.Println("  ✓ doctor: semua sehat — siap `tdocs start`.")
}

// upsertEnvFile updates KEY= lines in place, appends missing ones, keeps
// comments and unknown keys untouched. File is created 0600 when absent.
func upsertEnvFile(path string, vals map[string]string) error {
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		lines = strings.Split(string(data), "\n")
	}
	seen := map[string]bool{}
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			continue
		}
		key := strings.TrimSpace(trimmed[:strings.Index(trimmed, "=")])
		if v, wanted := vals[key]; wanted {
			lines[i] = key + "=" + quoteEnvValue(v)
			seen[key] = true
		}
	}
	for k, v := range vals {
		if !seen[k] {
			lines = append(lines, k+"="+quoteEnvValue(v))
		}
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0600)
}

func quoteEnvValue(v string) string {
	if v == "" || strings.ContainsAny(v, " \t#\"'") {
		return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'"
	}
	return v
}

// openBrowser opens url in the default browser when running interactively.
// Silent no-op over SSH, headless, or when TDOCS_NO_BROWSER/NO_BROWSER is set.
func openBrowser(url string) {
	if os.Getenv("TDOCS_NO_BROWSER") != "" || os.Getenv("NO_BROWSER") != "" {
		return
	}
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return
	}
	if fi, err := os.Stdout.Stat(); err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			if isWSL() {
				if _, err := exec.LookPath("wslview"); err == nil {
					cmd = exec.Command("wslview", url)
					break
				}
			}
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Start()
	}()
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}
