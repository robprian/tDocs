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
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var a, b int
		if i < len(pa) {
			a = pa[i]
		}
		if i < len(pb) {
			b = pb[i]
		}
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	return 0
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

// PackageName returns the release artifact for a package install kind
// (deb/rpm) on a GOARCH, matching scripts/package-{deb,rpm}.sh naming:
// tdocs_<ver>_<debarch>.deb / tdocs_<ver>_<rpmarch>.rpm.
// ok=false when the arch has no published package (caller falls back).
func PackageName(kind, version, goarch string) (string, bool) {
	ver := strings.TrimPrefix(version, "v")
	var arch string
	var ext string
	switch kind {
	case InstallDeb:
		var ok bool
		arch, ok = map[string]string{
			"amd64": "amd64", "arm64": "arm64", "386": "i386",
			"arm": "armhf", "ppc64le": "ppc64el", "s390x": "s390x",
		}[goarch]
		if !ok {
			return "", false
		}
		ext = "deb"
	case InstallRPM:
		var ok bool
		arch, ok = map[string]string{
			"amd64": "x86_64", "arm64": "aarch64", "386": "i686",
			"arm": "armv7hl", "ppc64le": "ppc64le", "s390x": "s390x",
		}[goarch]
		if !ok {
			return "", false
		}
		ext = "rpm"
	default:
		return "", false
	}
	return fmt.Sprintf("tdocs_%s_%s.%s", ver, arch, ext), true
}

// PackageManager returns the install command prefix for a package kind
// ("dnf" preferred, "yum"/"zypper"/"rpm -U" fallback for rpm).
func PackageManager(kind string) (string, []string) {
	if kind == InstallDeb {
		return "apt", []string{"install"}
	}
	for _, c := range []struct {
		bin  string
		args []string
	}{{"dnf", []string{"upgrade", "-y"}}, {"yum", []string{"upgrade", "-y"}}, {"zypper", []string{"install", "-y"}}, {"rpm", []string{"-Uvh"}}} {
		if _, err := exec.LookPath(c.bin); err == nil {
			return c.bin, c.args
		}
	}
	return "dnf", []string{"upgrade", "-y"}
}

// TarballName is the release artifact for a linux GOARCH. GOARCH arm ships
// as armv7 (GOARM=7 build); every other key is the artifact suffix verbatim.
func TarballName(version, goarch string) (string, bool) {
	arch, ok := map[string]string{
		"amd64": "amd64", "arm64": "arm64", "arm": "armv7",
		"386": "386", "ppc64le": "ppc64le", "s390x": "s390x",
	}[goarch]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("tdocs_%s_linux_%s.tar.gz",
		strings.TrimPrefix(version, "v"), arch), true
}

// UpgradeHint returns the exact command a human should run for this install
// kind. version is the release tag ("v2.1.1"), goarch is runtime.GOARCH.
// Names come from PackageName so the hint can never drift from real artifacts.
// The rpm hint is canonically `dnf upgrade`: PackageManager (used by the
// auto-install path) resolves the real binary at runtime, but the hint must
// stay deterministic across hosts.
func UpgradeHint(kind, version, goarch string) string {
	switch kind {
	case InstallDeb:
		if name, ok := PackageName(InstallDeb, version, goarch); ok {
			return fmt.Sprintf("sudo apt update && sudo apt install ./%s", name)
		}
		return "tdocs update"
	case InstallRPM:
		if name, ok := PackageName(InstallRPM, version, goarch); ok {
			return fmt.Sprintf("sudo dnf upgrade ./%s", name)
		}
		return "tdocs update"
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
