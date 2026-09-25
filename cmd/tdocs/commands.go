package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tdocs/internal/app"
	appcrypto "tdocs/internal/crypto"
	"tdocs/internal/db"
	"tdocs/internal/telegram"
	"tdocs/internal/web"
)

func runLogin(cfg *app.Config) {
	fmt.Println("  ◈ tDocs · Telegram pairing")
	fmt.Println("  ─────────────────────────────")
	database := openDatabase(cfg)
	defer database.Close()

	for _, arg := range os.Args[2:] {
		if arg == "--reset" || arg == "-r" {
			_ = database.DeleteSetting("telegram_session")
			_ = database.DeleteSetting("telegram_authorized")
			fmt.Println("Cleared stored Telegram session. Starting fresh authentication...")
			break
		}
	}

	// Fresh installs get a persistent random secret; installs with an existing
	// session keep the legacy key so the stored session stays decryptable.
	_, sessErr := database.GetSetting("telegram_session")
	cfg.SecretKey = app.EnsureSecretKey(cfg.DBPath, sessErr == nil)

	appID := cfg.TelegramAppID
	appHash := cfg.TelegramAppHash

	reader := bufio.NewReader(os.Stdin)

	// Check DB if not in config/env
	if appID == 0 {
		if stored, err := database.GetSetting("telegram_app_id"); err == nil && stored != "" {
			appID, _ = strconv.Atoi(stored)
		}
	}
	if appHash == "" {
		if stored, err := database.GetSetting("telegram_app_hash"); err == nil && stored != "" {
			appHash = stored
		}
	}

	if appID == 0 {
		fmt.Print("Enter your Telegram App ID (from https://my.telegram.org): ")
		val, _ := reader.ReadString('\n')
		appID, _ = strconv.Atoi(strings.TrimSpace(val))
		_ = database.SetSetting("telegram_app_id", strconv.Itoa(appID))
	}

	if appHash == "" {
		fmt.Print("Enter your Telegram App Hash (from https://my.telegram.org): ")
		val, _ := reader.ReadString('\n')
		appHash = strings.TrimSpace(val)
		_ = database.SetSetting("telegram_app_hash", appHash)
	}

	if appID == 0 || appHash == "" {
		fmt.Println("Error: Valid Telegram App ID and App Hash are required.")
		return
	}

	mgr := telegram.NewClientManager(database, appID, appHash, cfg.SecretKey)
	if cfg.StorageChannelID != 0 {
		mgr.SetConfiguredChannelID(cfg.StorageChannelID)
	}

	ctx := context.Background()
	err := mgr.Run(ctx, func(runCtx context.Context) error {
		if err := mgr.AuthenticateInteractive(runCtx, reader, cfg.DBPath); err != nil {
			return err
		}
		// Sync inside the same MTProto session: gotd's Client.Run
		// cannot be re-entered immediately after it closes (its
		// internal context stays canceled), so a second mgr.Run would
		// either hang or return "client already closed" and skip the
		// catalog sync entirely — the hang you saw after
		// "Using existing Storage Channel".
		if res, syncErr := mgr.SyncAndRecord(runCtx, "login"); syncErr != nil {
			fmt.Fprintf(os.Stderr, "  ○ Channel sync skipped: %v\n", syncErr)
		} else {
			fmt.Printf("  ✓ Channel sync: %d scanned, %d recovered, %d refreshed.\n", res.Scanned, res.Inserted, res.Updated)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Login failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "  Hint: wrong secret? Re-run with the original TDOCS_SECRET_KEY or `tdocs login --reset`.")
		return
	}

	fmt.Println("")
	fmt.Println("  ╭──────────────────────────────────────────────╮")
	fmt.Println("  │  ✓ Paired with Telegram — you're all set.   │")
	fmt.Println("  │  Next: tdocs server  → open the dashboard  │")
	fmt.Println("  ╰──────────────────────────────────────────────╯")
}

func runLogout(cfg *app.Config) {
	database := openDatabase(cfg)
	defer database.Close()
	_ = database.DeleteSetting("telegram_session")
	_ = database.DeleteSetting("telegram_authorized")
	fmt.Println("  ○ Session cleared. Run `tdocs login` to pair again.")
}

// runPasswd resets the dashboard password from the server console. This is the
// documented way out of a forgotten/rotated admin password: `--clear` removes
// the DB override so TDOCS_ADMIN_PASSWORD applies again, and a positional
// argument installs a fresh Argon2id hash.
func runPasswd(cfg *app.Config, args []string) {
	database := openDatabase(cfg)
	defer database.Close()

	clear := false
	newPass := ""
	for _, a := range args {
		switch {
		case a == "--clear" || a == "-c":
			clear = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "  Unknown flag: %s\n", a)
			return
		default:
			newPass = a
		}
	}

	switch {
	case clear && newPass != "":
		fmt.Fprintln(os.Stderr, "  Use either --clear or a new password, not both.")
		return
	case clear:
		if err := database.DeleteSetting("admin_password_hash"); err != nil {
			fmt.Fprintf(os.Stderr, "  Failed to clear password: %v\n", err)
			return
		}
		fmt.Println("  ✓ Password override cleared.")
		fmt.Println("    The dashboard now accepts TDOCS_ADMIN_PASSWORD (from env or .env).")
		return
	case newPass == "":
		fmt.Print(`  Usage: tdocs passwd <new-password>
         tdocs passwd --clear

    <new-password>  set a new dashboard password (min 8 chars, Argon2id)
    --clear         drop the stored password, fall back to TDOCS_ADMIN_PASSWORD
`)
		return
	case len(newPass) < 8:
		fmt.Fprintln(os.Stderr, "  New password must be at least 8 characters.")
		return
	}

	hash, err := appcrypto.HashPassword(newPass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Failed to hash password: %v\n", err)
		return
	}
	if err := database.SetSetting("admin_password_hash", hash); err != nil {
		fmt.Fprintf(os.Stderr, "  Failed to store password: %v\n", err)
		return
	}
	_ = database.Audit("cli", "password_change", "admin password reset via tdocs passwd", "local")
	fmt.Println("  ✓ Dashboard password updated (Argon2id).")
	fmt.Println("    Restart the server for a clean session list: tdocs server")
}

func runServer(cfg *app.Config) {
	database := openDatabase(cfg)
	defer database.Close()

	fmt.Print(bannerText())

	appID := cfg.TelegramAppID
	appHash := cfg.TelegramAppHash
	if appID == 0 {
		if stored, err := database.GetSetting("telegram_app_id"); err == nil {
			appID, _ = strconv.Atoi(stored)
		}
	}
	if appHash == "" {
		if stored, err := database.GetSetting("telegram_app_hash"); err == nil {
			appHash = stored
		}
	}

	// Telegram is optional for the HTTP surface: local folders, trash,
	// shares, and settings keep working without credentials (honest
	// NOT CONFIGURED state in the UI). Uploads/downloads need pairing.
	var mgr *telegram.ClientManager
	if appID != 0 && appHash != "" {
		mgr = telegram.NewClientManager(database, appID, appHash, cfg.SecretKey)
		if cfg.StorageChannelID != 0 {
			mgr.SetConfiguredChannelID(cfg.StorageChannelID)
		}
	} else {
		fmt.Println("  ○ Telegram credentials not set — dashboard starts in degraded mode.")
		fmt.Println("    Run `tdocs setup` then `tdocs login` to enable storage features.")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if mgr != nil {
		// Start MTProto client in background. Corrupt sessions self-heal to
		// ErrNotFound inside LoadSession, so a stale/rotated secret surfaces here
		// as "not authenticated" instead of killing the dashboard.
		go func() {
			err := mgr.Run(ctx, func(runCtx context.Context) error {
				if !mgr.CheckAuthorized(runCtx) {
					fmt.Fprintln(os.Stderr, "  ✕ Telegram session is not authorized (uploads/downloads will fail).")
					fmt.Fprintln(os.Stderr, "  Hint: `tdocs login` (or `tdocs login --reset` after a secret change), then restart.")
				} else if err := mgr.EnsureStorageChannel(runCtx); err != nil {
					fmt.Fprintf(os.Stderr, "  ✕ Storage channel unavailable (uploads/downloads will fail): %v\n", err)
					fmt.Fprintln(os.Stderr, "  Hint: `tdocs login` (or `tdocs login --reset` after a secret change), then restart.")
				} else if res, syncErr := mgr.SyncAndRecord(runCtx, "startup"); syncErr != nil {
					fmt.Fprintf(os.Stderr, "  ○ Channel sync skipped: %v\n", syncErr)
				} else if res.Inserted > 0 || res.Updated > 0 {
					fmt.Printf("  ✓ Channel sync: %d scanned, %d recovered, %d refreshed.\n", res.Scanned, res.Inserted, res.Updated)
				}
				<-runCtx.Done()
				return nil
			})
			if err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "  ✕ MTProto runner stopped: %v\n", err)
			}
		}()
	}

	startPort, _ := strconv.Atoi(cfg.Port)
	if startPort <= 0 {
		startPort = 8080
	}

	listener, boundPort, err := web.FindAvailableListener(cfg.Host, startPort, 50)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Network error: %v\n", err)
		return
	}
	defer listener.Close()

	if boundPort != startPort {
		fmt.Printf("  ○ Port %d busy → using %d instead.\n", startPort, boundPort)
	}
	cfg.Port = strconv.Itoa(boundPort)

	srv, err := web.NewServer(cfg, database, mgr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Web server init failed: %v\n", err)
		return
	}

	// Resume built-in HTTPS when a custom domain was saved in Settings.
	srv.StartAutoTLSFromSettings(ctx)

	// Start background periodic snapshot scheduler
	srv.StartPeriodicBackup(ctx)

	// Non-blocking release check: warms the dashboard update banner and
	// logs to the journal when a newer release exists.
	srv.CheckForUpdatesAsync()

	httpServer := &http.Server{
		Handler: srv,
	}

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n  ○ Shutting down — writing final snapshot…")
		shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer sCancel()
		_ = srv.PerformAutomatedSnapshot(shutdownCtx)
		_ = httpServer.Shutdown(shutdownCtx)
		cancel()
	}()

	printServerPanel(cfg, boundPort, database)
	openBrowser(fmt.Sprintf("http://localhost:%d", boundPort))

	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		fmt.Println("  ● TLS enabled (https).")
		if err := httpServer.ServeTLS(listener, cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "  ✕ HTTPS server error: %v\n", err)
		}
		return
	}
	if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "  ✕ HTTP server error: %v\n", err)
	}
}

func printServerPanel(cfg *app.Config, port int, database *db.DB) {
	sessionOK := false
	channelOK := false
	if database != nil {
		if _, err := database.GetSetting("telegram_authorized"); err == nil {
			sessionOK = true
		}
		if _, err := database.GetSetting("storage_channel_id"); err == nil {
			channelOK = true
		}
	}
	yes := func(ok bool) string {
		if ok {
			return "connected"
		}
		return "missing — run `tdocs login`"
	}

	fmt.Println("")
	fmt.Println("  ╭─ ◈ tDocs is running ─────────────────────────────────────╮")
	fmt.Printf("  │  Dashboard   http://localhost:%-5d                          │\n", port)
	for _, ip := range web.GetLocalIPs() {
		fmt.Printf("  │  Network     http://%s:%d", ip, port)
		// pad to box width (best-effort, no unicode width math)
		fmt.Println("  │")
	}
	fmt.Println("  │                                                            │")
	fmt.Printf("  │  Telegram    %-44s  │\n", yes(sessionOK))
	fmt.Printf("  │  Vault       %-44s  │\n", yes(channelOK))
	fmt.Println("  │                                                            │")
	fmt.Println("  │  API index   GET /api                                      │")
	fmt.Println("  │  Swagger     GET /docs                                     │")
	fmt.Println("  │  Contract    GET /api/openapi.json                         │")
	fmt.Println("  │  Catalog     GET /api/cdn/files?mime=video/                │")
	fmt.Println("  │  Embed       GET /cdn/{id}/stream (Range 206)              │")
	fmt.Println("  │  Auth        Bearer <admin password> / cookie              │")
	fmt.Println("  │                                                            │")
	fmt.Println("  │  Safe Mode   1 upload · 30 ms pace · FloodWait             │")
	fmt.Println("  │  Admin key   set (hidden — never printed to logs)          │")
	fmt.Println("  ╰────────────────────────────────────────────────────────────╯")
	fmt.Println("  Press Ctrl+C to stop. Logs below.")
	fmt.Println("")
}

func runUpload(cfg *app.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: tdocs upload <filepath> [--folder <folder_id>]")
		return
	}

	filePath := args[0]
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		fmt.Printf("Error accessing file %s: %v\n", filePath, err)
		return
	}

	var folderID *string
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--folder" {
			val := args[i+1]
			folderID = &val
		}
	}

	f, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer f.Close()

	database := openDatabase(cfg)
	defer database.Close()

	appID := cfg.TelegramAppID
	appHash := cfg.TelegramAppHash
	if appID == 0 {
		if stored, err := database.GetSetting("telegram_app_id"); err == nil {
			appID, _ = strconv.Atoi(stored)
		}
	}
	if appHash == "" {
		if stored, err := database.GetSetting("telegram_app_hash"); err == nil {
			appHash = stored
		}
	}

	if appID == 0 || appHash == "" {
		fmt.Println("  ✕ Telegram credentials missing. Run `tdocs login` first.")
		return
	}

	mgr := telegram.NewClientManager(database, appID, appHash, cfg.SecretKey)
	if cfg.StorageChannelID != 0 {
		mgr.SetConfiguredChannelID(cfg.StorageChannelID)
	}
	limiter := telegram.NewSafeLimiter()

	fileName := filepath.Base(filePath)
	mimeType := detectMimeType(fileName)
	size := fileInfo.Size()

	fmt.Printf("  ◈ Uploading %s (%.2f MB) → Storage Channel…\n", fileName, float64(size)/(1024*1024))

	ctx := context.Background()
	err = mgr.Run(ctx, func(runCtx context.Context) error {
		if err := mgr.EnsureStorageChannel(runCtx); err != nil {
			return err
		}

		msgID, docID, accessHash, shaHex, err := mgr.UploadFromReader(runCtx, f, size, fileName, mimeType, limiter, func(uploaded, total int64) {
			pct := float64(uploaded) / float64(total) * 100
			filled := int(pct / 5)
			bar := ""
			for i := range 20 {
				if i < filled {
					bar += "●"
				} else {
					bar += "○"
				}
			}
			fmt.Printf("\r  [%s] %5.1f%% (%d/%d)", bar, pct, uploaded, total)
		})
		if err != nil {
			return err
		}

		fmt.Println("\n  ○ Finalizing database record…")
		_, err = database.CreateFile(folderID, fileName, size, mimeType, msgID, strconv.FormatInt(docID, 10), strconv.FormatInt(accessHash, 10), shaHex)
		return err
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n  ✕ Upload failed: %v\n", err)
		return
	}

	fmt.Println("  ╭──────────────────────────────────────────────╮")
	fmt.Println("  │  ✓ Upload complete — stored in Vault.        │")
	fmt.Println("  ╰──────────────────────────────────────────────╯")
}

func detectMimeType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".tar", ".gz":
		return "application/gzip"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

func runDownload(cfg *app.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: tdocs download <file_id> [--output <path>]")
		return
	}

	fileID := args[0]
	database := openDatabase(cfg)
	defer database.Close()

	fileRecord, err := database.GetFile(fileID)
	if err != nil {
		fmt.Printf("Error: File not found with ID %s\n", fileID)
		return
	}

	outputPath := fileRecord.Name
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--output" {
			outputPath = args[i+1]
		}
	}

	appID := cfg.TelegramAppID
	appHash := cfg.TelegramAppHash
	if appID == 0 {
		if stored, err := database.GetSetting("telegram_app_id"); err == nil {
			appID, _ = strconv.Atoi(stored)
		}
	}
	if appHash == "" {
		if stored, err := database.GetSetting("telegram_app_hash"); err == nil {
			appHash = stored
		}
	}

	if appID == 0 || appHash == "" {
		fmt.Println("  ✕ Telegram credentials missing. Run `tdocs login` first.")
		return
	}

	out, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Error creating output file %s: %v\n", outputPath, err)
		return
	}
	defer out.Close()

	docID, _ := strconv.ParseInt(fileRecord.TelegramFileID, 10, 64)
	docHash, _ := strconv.ParseInt(fileRecord.TelegramAccessHash, 10, 64)

	mgr := telegram.NewClientManager(database, appID, appHash, cfg.SecretKey)
	if cfg.StorageChannelID != 0 {
		mgr.SetConfiguredChannelID(cfg.StorageChannelID)
	}

	fmt.Printf("  ◈ Downloading %s (%.2f MB) → %s…\n", fileRecord.Name, float64(fileRecord.Size)/(1024*1024), outputPath)

	ctx := context.Background()
	err = mgr.Run(ctx, func(runCtx context.Context) error {
		// Fresh coordinates first (reference rotation), stored ones as fallback.
		if freshed, rerr := mgr.ResolveChannelDocument(runCtx, fileRecord.TelegramMessageID); rerr == nil {
			return mgr.DownloadFullLoc(runCtx, freshed.Location(), out)
		}
		return mgr.DownloadFull(runCtx, docID, docHash, out)
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "  ✕ Download failed: %v\n", err)
		return
	}

	fmt.Printf("  ✓ Saved → %s\n", outputPath)
}

func runList(cfg *app.Config, args []string) {
	database := openDatabase(cfg)
	defer database.Close()

	folders, err := database.ListFolders(nil)
	if err != nil {
		fmt.Printf("Error listing folders: %v\n", err)
		return
	}
	files, err := database.ListFiles(nil)
	if err != nil {
		fmt.Printf("Error listing files: %v\n", err)
		return
	}

	fmt.Println("  ◈ Root Folders ─────────────────")
	if len(folders) == 0 {
		fmt.Println("  (empty)")
	}
	for _, f := range folders {
		fmt.Printf("  ◈ %s  ·  %s\n", f.Name, f.ID)
	}
	fmt.Println("  ◈ Root Files ───────────────────")
	if len(files) == 0 {
		fmt.Println("  (empty)")
	}
	for _, f := range files {
		fmt.Printf("  ● %s  ·  %d bytes  ·  %s\n", f.Name, f.Size, f.ID)
	}
}

func runBackup(cfg *app.Config) {
	fmt.Println("Exporting SQLite snapshot to Telegram Storage Channel...")
	database := openDatabase(cfg)

	gzPath, err := database.CreateSnapshot()
	if err != nil {
		database.Close()
		fmt.Printf("Failed to create snapshot: %v\n", err)
		return
	}
	defer os.Remove(gzPath)

	appID := cfg.TelegramAppID
	appHash := cfg.TelegramAppHash
	if appID == 0 {
		if stored, err := database.GetSetting("telegram_app_id"); err == nil {
			appID, _ = strconv.Atoi(stored)
		}
	}
	if appHash == "" {
		if stored, err := database.GetSetting("telegram_app_hash"); err == nil {
			appHash = stored
		}
	}

	database.Close()

	if appID == 0 || appHash == "" {
		fmt.Println("Telegram credentials not configured. Please run `tdocs login` first.")
		return
	}

	database = openDatabase(cfg)
	defer database.Close()

	mgr := telegram.NewClientManager(database, appID, appHash, cfg.SecretKey)
	if cfg.StorageChannelID != 0 {
		mgr.SetConfiguredChannelID(cfg.StorageChannelID)
	}
	limiter := telegram.NewSafeLimiter()

	ctx := context.Background()
	err = mgr.Run(ctx, func(runCtx context.Context) error {
		if err := mgr.EnsureStorageChannel(runCtx); err != nil {
			return err
		}
		msgID, err := mgr.UploadSnapshot(runCtx, gzPath, limiter)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Backup snapshot successfully uploaded & pinned (Message ID: %d)\n", msgID)
		return nil
	})

	if err != nil {
		fmt.Printf("Backup failed: %v\n", err)
	}
}

func runRestore(cfg *app.Config) {
	fmt.Printf("Restoring SQLite snapshot to %s from Telegram...\n", cfg.DBPath)

	database := openDatabase(cfg)

	appID := cfg.TelegramAppID
	appHash := cfg.TelegramAppHash
	if appID == 0 {
		if stored, err := database.GetSetting("telegram_app_id"); err == nil {
			appID, _ = strconv.Atoi(stored)
		}
	}
	if appHash == "" {
		if stored, err := database.GetSetting("telegram_app_hash"); err == nil {
			appHash = stored
		}
	}

	reader := bufio.NewReader(os.Stdin)
	if appID == 0 || appHash == "" {
		fmt.Print("Enter your Telegram App ID: ")
		val, _ := reader.ReadString('\n')
		appID, _ = strconv.Atoi(strings.TrimSpace(val))
		fmt.Print("Enter your Telegram App Hash: ")
		val, _ = reader.ReadString('\n')
		appHash = strings.TrimSpace(val)
	}

	mgr := telegram.NewClientManager(database, appID, appHash, cfg.SecretKey)
	if cfg.StorageChannelID != 0 {
		mgr.SetConfiguredChannelID(cfg.StorageChannelID)
	}

	ctx := context.Background()
	err := mgr.Run(ctx, func(runCtx context.Context) error {
		storedID, _ := database.GetSetting("storage_channel_id")
		storedHash, _ := database.GetSetting("storage_channel_hash")

		if storedID != "" && storedHash != "" {
			cID, _ := strconv.ParseInt(storedID, 10, 64)
			cHash, _ := strconv.ParseInt(storedHash, 10, 64)
			mgr.SetStorageChannel(cID, cHash)
		} else if cfg.StorageChannelID != 0 {
			hash, err := mgr.ResolveChannelByID(runCtx, cfg.StorageChannelID)
			if err != nil {
				return fmt.Errorf("could not resolve configured channel ID %d: %w", cfg.StorageChannelID, err)
			}
			mgr.SetStorageChannel(cfg.StorageChannelID, hash)
		} else {
			fmt.Println("Searching for the storage vault channel on Telegram...")
			candidates, err := mgr.DiscoverStorageChannels(runCtx)
			if err != nil || len(candidates) == 0 {
				return fmt.Errorf("no storage vault channel found in your Telegram account to restore from")
			}
			var chosen *telegram.DiscoveredChannel
			if len(candidates) == 1 {
				chosen = &candidates[0]
			} else {
				fmt.Printf("\nFound %d Storage Channels:\n", len(candidates))
				for i, c := range candidates {
					snapText := "no snapshots"
					if c.SnapshotCount > 0 {
						snapText = fmt.Sprintf("%d snapshot(s), latest: %s", c.SnapshotCount, c.LatestSnapshot.CreatedAt.Format("2006-01-02 15:04"))
					}
					fmt.Printf("  [%d] Channel ID: %d | Created: %s | %s\n", i+1, c.ID, c.CreatedAt.Format("2006-01-02"), snapText)
				}
				fmt.Printf("Select channel to restore from (1-%d): ", len(candidates))
				choiceStr, _ := reader.ReadString('\n')
				idx, _ := strconv.Atoi(strings.TrimSpace(choiceStr))
				if idx < 1 || idx > len(candidates) {
					idx = 1
				}
				chosen = &candidates[idx-1]
			}
			mgr.SetStorageChannel(chosen.ID, chosen.AccessHash)
		}

		// Close database connection before overwriting file
		database.Close()
		return mgr.RestoreLatestSnapshot(runCtx, cfg.DBPath)
	})

	if err != nil {
		fmt.Printf("Restore failed: %v\n", err)
		return
	}

	fmt.Println("✓ Database successfully restored from Telegram Storage Channel!")
}
