package app

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Host             string
	Port             string
	DBPath           string
	SecretKey        string
	TelegramAppID    int
	TelegramAppHash  string
	StorageChannelID int64
	AdminPassword    string
	CDNBaseURL       string
	CDNPublic        bool
	TLSCertFile      string
	TLSKeyFile       string
}

// LoadConfig reads configuration from environment variables with safe defaults.
// It first loads a local `.env` file (if present) without overriding real env vars,
// then resolves the MTProto session secret via ResolveSecretKey semantics.
func LoadConfig() *Config {
	loadDotEnv(".env")

	host := env3("TDOCS_HOST", "ROBDOCS_HOST", "TELEDRIVE_HOST", "0.0.0.0")
	port := env3("TDOCS_PORT", "ROBDOCS_PORT", "TELEDRIVE_PORT", "8080")
	dbPath := resolveDBPath()
	adminPass := env3("TDOCS_ADMIN_PASSWORD", "ROBDOCS_ADMIN_PASSWORD", "TELEDRIVE_ADMIN_PASSWORD", "admin123")

	appID, _ := strconv.Atoi(firstSet("TDOCS_TG_APP_ID", "ROBDOCS_TG_APP_ID", "TELEDRIVE_TG_APP_ID", "app_api_id", "0"))
	appHash := firstSet("TDOCS_TG_APP_HASH", "ROBDOCS_TG_APP_HASH", "TELEDRIVE_TG_APP_HASH", "app_api_hash", "")
	channelID, _ := strconv.ParseInt(env3("TDOCS_STORAGE_CHANNEL_ID", "ROBDOCS_STORAGE_CHANNEL_ID", "TELEDRIVE_STORAGE_CHANNEL_ID", "0"), 10, 64)

	return &Config{
		Host:             host,
		Port:             port,
		DBPath:           dbPath,
		SecretKey:        ResolveSecretKey(dbPath),
		TelegramAppID:    appID,
		TelegramAppHash:  appHash,
		StorageChannelID: channelID,
		AdminPassword:    adminPass,
		CDNBaseURL:       strings.TrimSuffix(env3("TDOCS_CDN_BASE_URL", "ROBDOCS_CDN_BASE_URL", "TELEDRIVE_CDN_BASE_URL", ""), "/"),
		CDNPublic:        env3("TDOCS_CDN_PUBLIC", "ROBDOCS_CDN_PUBLIC", "TELEDRIVE_CDN_PUBLIC", "true") != "false",
		TLSCertFile:      env3("TDOCS_TLS_CERT_FILE", "ROBDOCS_TLS_CERT_FILE", "TELEDRIVE_TLS_CERT_FILE", ""),
		TLSKeyFile:       env3("TDOCS_TLS_KEY_FILE", "ROBDOCS_TLS_KEY_FILE", "TELEDRIVE_TLS_KEY_FILE", ""),
	}
}

// resolveDBPath defaults to tdocs.db but keeps using a pre-rebrand
// robdocs.db / teledrive.db when that is the only database present.
func resolveDBPath() string {
	if v := os.Getenv("TDOCS_DB_PATH"); v != "" {
		return v
	}
	if v := os.Getenv("ROBDOCS_DB_PATH"); v != "" {
		return v
	}
	if v := os.Getenv("TELEDRIVE_DB_PATH"); v != "" {
		return v
	}
	if _, err := os.Stat("tdocs.db"); err == nil {
		return "tdocs.db"
	}
	if _, err := os.Stat("robdocs.db"); err == nil {
		return "robdocs.db"
	}
	if _, err := os.Stat("teledrive.db"); err == nil {
		return "teledrive.db"
	}
	return "tdocs.db"
}

// ResolveSecretKey implements the SECURITY.md key hierarchy without breaking
// existing installs whose sessions were encrypted with the legacy default:
// env TDOCS_SECRET_KEY > ROBDOCS_SECRET_KEY > TELEDRIVE_SECRET_KEY >
// .tdocs.key > .robdocs.key > .teledrive.key > legacy default.
func ResolveSecretKey(dbPath string) string {
	if v := os.Getenv("TDOCS_SECRET_KEY"); v != "" {
		return v
	}
	if v := os.Getenv("ROBDOCS_SECRET_KEY"); v != "" {
		return v
	}
	if v := os.Getenv("TELEDRIVE_SECRET_KEY"); v != "" {
		return v
	}
	for _, p := range keyFilePaths(dbPath) {
		if key, err := os.ReadFile(p); err == nil {
			if k := strings.TrimSpace(string(key)); k != "" {
				return k
			}
		}
	}
	return "teledrive-default-local-secret-32b"
}

// EnsureSecretKey generates and persists a random .tdocs.key (0600) when no
// secret is configured yet. Call after opening the DB on fresh installs only:
// if hasSession is true the legacy default must keep working, so do nothing.
func EnsureSecretKey(dbPath string, hasSession bool) string {
	if v := os.Getenv("TDOCS_SECRET_KEY"); v != "" {
		return v
	}
	if v := os.Getenv("ROBDOCS_SECRET_KEY"); v != "" {
		return v
	}
	if v := os.Getenv("TELEDRIVE_SECRET_KEY"); v != "" {
		return v
	}
	paths := keyFilePaths(dbPath)
	for _, p := range paths {
		if key, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(key)) != "" {
			return strings.TrimSpace(string(key))
		}
	}
	if hasSession {
		return "teledrive-default-local-secret-32b"
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "teledrive-default-local-secret-32b"
	}
	key := hex.EncodeToString(buf)
	path := paths[0]
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(key+"\n"), 0o600)
	return key
}

func keyFilePaths(dbPath string) []string {
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		return []string{filepath.Join(dir, ".tdocs.key"), filepath.Join(dir, ".robdocs.key"), filepath.Join(dir, ".teledrive.key")}
	}
	return []string{".tdocs.key", ".robdocs.key", ".teledrive.key"}
}

func keyFilePath(dbPath string) string { return keyFilePaths(dbPath)[0] }

// env2 reads TDOCS_ first, then the legacy ROBDOCS_ name.
func env2(primary, fallback, def string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	return getEnv(fallback, def)
}

// env3 reads TDOCS_ first, then ROBDOCS_, then the legacy TELEDRIVE_ name.
func env3(primary, mid, legacy, def string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	if v := os.Getenv(mid); v != "" {
		return v
	}
	return getEnv(legacy, def)
}

// loadDotEnv parses KEY=VALUE lines (ignoring comments/blanks, stripping quotes)
// and exports keys that are not already set in the environment.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			v = strings.Trim(v, `"'`)
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
	}
}

// firstSet returns the first non-empty value among env keys, else fallback.
func firstSet(keys ...string) string {
	for _, k := range keys[:len(keys)-1] {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return keys[len(keys)-1]
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
