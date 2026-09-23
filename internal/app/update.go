package app

// Shared update machinery: GitHub release lookup, version comparison, install
// kind detection, and per-kind upgrade hints. Used by both the `tdocs update`
// CLI and the dashboard update banner (GET /api/update/status), so the two
// surfaces can never disagree about "is there a newer release?".

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReleaseRepo is the canonical source of published tDocs releases.
const ReleaseRepo = "robprian/tDocs"

// ReleaseAsset is one downloadable file attached to a GitHub release.
type ReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// ReleaseInfo is the subset of a GitHub release the updater cares about.
type ReleaseInfo struct {
	Tag         string         `json:"tag_name"`
	PublishedAt time.Time      `json:"published_at"`
	Assets      []ReleaseAsset `json:"assets"`
}

// AssetURL returns the download URL of the named asset, or "".
func (r *ReleaseInfo) AssetURL(name string) string {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

// FetchLatestRelease queries the GitHub API for the latest published release.
// A GITHUB_TOKEN env var is used when present (higher rate limits).
func FetchLatestRelease(ctx context.Context) (*ReleaseInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+ReleaseRepo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tdocs-update/check")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API HTTP %d", resp.StatusCode)
	}
	var rel ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.Tag == "" {
		return nil, fmt.Errorf("GitHub API returned a release without a tag")
	}
	return &rel, nil
}

// CompareVersions compares two version strings ("v2.1.0", "2.1.0", "dev").
// Returns -1, 0, +1 for a<b, a==b, a>b. "dev"/empty/unknown sorts below any
// numeric release so dev builds always see releases as newer.
func CompareVersions(a, b string) int {
	pa := versionParts(a)
	pb := versionParts(b)
	if pa == nil && pb == nil {
		return 0
	}
	if pa == nil {
		return -1
	}
	if pb == nil {
		return 1
	}
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	default:
		return 0
	}
}

func versionParts(v string) []int {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	if v == "" {
		return nil
	}
	chunks := strings.Split(v, ".")
	parts := make([]int, 0, len(chunks))
	for _, c := range chunks {
		digits := ""
		for _, r := range c {
			if r < '0' || r > '9' {
				break
			}
			digits += string(r)
		}
		if digits == "" {
			return nil
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return nil
		}
		parts = append(parts, n)
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}

// IsNewer reports whether the release tag is newer than the running version.
// current is the display version ("v2.1.0" or "dev").
func IsNewer(current, latestTag string) bool {
	return CompareVersions(current, latestTag) < 0
}

// Install kinds returned by InstallKind.
const (
	InstallDeb     = "deb"
	InstallRPM     = "rpm"
	InstallTarball = "tarball"
	InstallSource  = "source"
	InstallUnknown = "unknown"
)

// InstallKind detects how the running binary was installed, so upgrade
// guidance (and guards) match reality: package installs must go through the
// system package manager, never an in-place binary swap.
func InstallKind(exePath, mode string) string {
	if mode == "dev" {
		return InstallSource
	}
	exe := exePath
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	if exe == "/usr/bin/tdocs" || strings.HasPrefix(exe, "/usr/bin/") ||
		strings.HasPrefix(exe, "/usr/local/bin/") {
		if ownedByDPKG(exe) {
			return InstallDeb
		}
		if ownedByRPM(exe) {
			return InstallRPM
		}
		return InstallTarball // manually placed system binary
	}
	return InstallTarball
}

func ownedByDPKG(path string) bool {
	if _, err := exec.LookPath("dpkg-query"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "dpkg-query", "-S", path).Run() == nil
}

func ownedByRPM(path string) bool {
	if _, err := exec.LookPath("rpm"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "rpm", "-qf", path).Run() == nil
}

// TarballName is the release artifact for a linux GOARCH (amd64/arm64 only;
// other arches ship tarballs too but the self-updater covers the mainstream).
func TarballName(version, goarch string) (string, bool) {
	switch goarch {
	case "amd64", "arm64":
		return fmt.Sprintf("tdocs_%s_linux_%s.tar.gz",
			strings.TrimPrefix(version, "v"), goarch), true
	default:
		return "", false
	}
}

// UpgradeHint returns the exact command a human should run for this install
// kind. version is the release tag ("v2.1.1"), goarch is runtime.GOARCH.
func UpgradeHint(kind, version, goarch string) string {
	ver := strings.TrimPrefix(version, "v")
	switch kind {
	case InstallDeb:
		debArch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[goarch]
		if debArch == "" {
			debArch = "amd64"
		}
		return fmt.Sprintf("sudo apt update && sudo apt install ./tdocs_%s_%s.deb", ver, debArch)
	case InstallRPM:
		rpmArch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
		if rpmArch == "" {
			rpmArch = "x86_64"
		}
		return fmt.Sprintf("sudo dnf upgrade ./tdocs_%s_%s.rpm", ver, rpmArch)
	case InstallSource:
		return "git pull && make build"
	default:
		return "tdocs update"
	}
}

// ReleaseURL returns the human release page for a tag.
func ReleaseURL(tag string) string {
	return "https://github.com/" + ReleaseRepo + "/releases/tag/" + tag
}
