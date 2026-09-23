package app

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.1.0", "v2.1.0", 0},
		{"2.1.0", "v2.1.0", 0},
		{"v2.1.0", "v2.1.1", -1},
		{"v2.1.1", "v2.1.0", 1},
		{"v2.1.0", "v2.2.0", -1},
		{"v2.10.0", "v2.9.9", 1},
		{"v3.0.0", "v2.99.99", 1},
		{"dev", "v2.1.0", -1},
		{"", "v2.1.0", -1},
		{"v2.1.0", "dev", 1},
		{"dev", "dev", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("v2.1.0", "v2.1.1") {
		t.Error("v2.1.1 should be newer than v2.1.0")
	}
	if IsNewer("v2.1.1", "v2.1.1") {
		t.Error("same version must not count as newer")
	}
	if IsNewer("v2.1.1", "v2.1.0") {
		t.Error("older release must not count as newer")
	}
	if !IsNewer("dev", "v2.1.0") {
		t.Error("dev build should always see releases as newer")
	}
}

func TestUpgradeHint(t *testing.T) {
	if got := UpgradeHint(InstallDeb, "v2.1.1", "amd64"); got != "sudo apt update && sudo apt install ./tdocs_2.1.1_amd64.deb" {
		t.Errorf("deb hint: %q", got)
	}
	if got := UpgradeHint(InstallRPM, "v2.1.1", "amd64"); got != "sudo dnf upgrade ./tdocs_2.1.1_x86_64.rpm" {
		t.Errorf("rpm hint: %q", got)
	}
	if got := UpgradeHint(InstallTarball, "v2.1.1", "amd64"); got != "tdocs update" {
		t.Errorf("tarball hint: %q", got)
	}
	if got := UpgradeHint(InstallSource, "v2.1.1", "amd64"); got == "" || got == "tdocs update" {
		t.Errorf("source hint should be a build command, got %q", got)
	}
}

func TestTarballName(t *testing.T) {
	if name, ok := TarballName("v2.1.1", "amd64"); !ok || name != "tdocs_2.1.1_linux_amd64.tar.gz" {
		t.Errorf("amd64 tarball: %q %v", name, ok)
	}
	if _, ok := TarballName("v2.1.1", "s390x"); ok {
		t.Error("self-updater must not claim s390x tarball support")
	}
}

func TestInstallKind(t *testing.T) {
	if got := InstallKind("/root/build/tdocs", "dev"); got != InstallSource {
		t.Errorf("dev mode should report source, got %q", got)
	}
	// Non-system path without package ownership is a tarball/manual install.
	if got := InstallKind("/tmp/tdocs-test", "user"); got != InstallTarball {
		t.Errorf("expected tarball, got %q", got)
	}
}
