#!/usr/bin/env bash
# Build an RPM package: tdocs_<version>_<rpmarch>.rpm
#
# Works for x86_64 and aarch64 (and other arches when rpmbuild supports them).
# Suitable for RHEL/Rocky/Alma/CentOS/Fedora and openSUSE/SUSE (RPM format).
#
# Usage: scripts/package-rpm.sh <version> <goarch> [bindir] [outdir]
set -euo pipefail

VERSION_RAW="${1:?version required}"
GOARCH="${2:?goarch required}"
VERSION="${VERSION_RAW#v}"
BINDIR="${3:-}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# GOARCH → RPM architecture
case "$GOARCH" in
  amd64)   RPMARCH=x86_64 ;;
  arm64)   RPMARCH=aarch64 ;;
  386)     RPMARCH=i686 ;;
  arm)     RPMARCH=armv7hl ;;
  ppc64le) RPMARCH=ppc64le ;;
  s390x)   RPMARCH=s390x ;;
  *) echo "unsupported goarch for rpm: $GOARCH" >&2; exit 1 ;;
esac

if [ -z "$BINDIR" ]; then
  case "$GOARCH" in
    arm) BINDIR="dist/linux/armv7" ;;
    *)   BINDIR="dist/linux/$GOARCH" ;;
  esac
fi
OUTDIR="${4:-dist/rpm/$RPMARCH}"

BIN="$BINDIR/tdocs"
if [ ! -f "$BIN" ]; then
  echo "missing binary: $BIN" >&2
  exit 1
fi

if ! command -v rpmbuild >/dev/null 2>&1; then
  echo "rpmbuild not found — install the 'rpm' package (apt install rpm / dnf install rpm-build)" >&2
  exit 1
fi

TOP="$(mktemp -d)"
trap 'rm -rf "$TOP"' EXIT

mkdir -p "$TOP"/{BUILD,RPMS,SOURCES,SPECS,SRPMS}
PKGDIR="$TOP/SOURCES/tdocs-$VERSION"
mkdir -p "$PKGDIR/systemd" "$PKGDIR/etc/tdocs"

install -m 755 "$BIN" "$PKGDIR/tdocs"
install -m 644 packaging/systemd/tdocs.service "$PKGDIR/systemd/tdocs.service"
install -m 644 LICENSE "$PKGDIR/LICENSE"
install -m 644 README.md "$PKGDIR/README.md"
install -m 644 .env.example "$PKGDIR/env.example"

# tar the source tree rpmbuild expects
tar -C "$TOP/SOURCES" -czf "$TOP/SOURCES/tdocs-$VERSION.tar.gz" "tdocs-$VERSION"
rm -rf "$PKGDIR"

SPEC="$TOP/SPECS/tdocs.spec"
cat > "$SPEC" <<EOF
# Prebuilt static binary — no compilation and no debuginfo extraction.
%define debug_package %{nil}
%define _build_id_links none
Name:           tdocs
Version:        $VERSION
Release:        1%{?dist}
Summary:        Self-hosted personal cloud storage using Telegram MTProto
License:        MIT
URL:            https://github.com/robprian/tDocs
Source0:        tdocs-%{version}.tar.gz
BuildArch:      $RPMARCH
# Static pure-Go binary: no runtime library dependencies.
AutoReqProv:    no
# MTProto (Telegram) and 'tdocs update' (GitHub API) need TLS trust anchors.
Requires:       ca-certificates

%description
tDocs bridges a web dashboard and virtual file system to Telegram's
MTProto infrastructure as a high-capacity object store. Single static
binary with embedded frontend — no Go, Node.js, or database server
required at runtime.

%prep
%setup -q

%build
# Nothing to compile — prebuilt static binary ships in the tarball.

%install
rm -rf %{buildroot}
mkdir -p %{buildroot}/usr/bin
mkdir -p %{buildroot}/usr/lib/systemd/system
mkdir -p %{buildroot}/etc/tdocs
mkdir -p %{buildroot}/usr/share/doc/tdocs
install -m 755 tdocs %{buildroot}/usr/bin/tdocs
install -m 644 systemd/tdocs.service %{buildroot}/usr/lib/systemd/system/tdocs.service
install -m 644 LICENSE %{buildroot}/usr/share/doc/tdocs/LICENSE
install -m 644 README.md %{buildroot}/usr/share/doc/tdocs/README.md
install -m 644 env.example %{buildroot}/usr/share/doc/tdocs/env.example

%pre
getent group tdocs >/dev/null 2>&1 || groupadd --system tdocs 2>/dev/null || true
getent passwd tdocs >/dev/null 2>&1 || useradd --system --gid tdocs --home-dir /var/lib/tdocs --shell /sbin/nologin tdocs 2>/dev/null || true
mkdir -p /var/lib/tdocs /etc/tdocs
chown tdocs:tdocs /var/lib/tdocs 2>/dev/null || true
chmod 750 /var/lib/tdocs 2>/dev/null || true
chmod 750 /etc/tdocs 2>/dev/null || true
exit 0

%post
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
if [ ! -f /etc/tdocs/.env ] && [ -f /usr/share/doc/tdocs/env.example ]; then
  cp /usr/share/doc/tdocs/env.example /etc/tdocs/.env 2>/dev/null || true
  chmod 600 /etc/tdocs/.env 2>/dev/null || true
  chown root:tdocs /etc/tdocs/.env 2>/dev/null || true
fi
exit 0

%preun
if [ \$1 -eq 0 ] && [ -d /run/systemd/system ]; then
  systemctl disable --now tdocs.service >/dev/null 2>&1 || true
fi
exit 0

%postun
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
# Data in /var/lib/tdocs and config in /etc/tdocs are intentionally kept.

%files
%defattr(-,root,root,-)
/usr/bin/tdocs
/usr/lib/systemd/system/tdocs.service
%dir %attr(750,root,tdocs) /etc/tdocs
%doc /usr/share/doc/tdocs/README.md
%doc /usr/share/doc/tdocs/LICENSE
%doc /usr/share/doc/tdocs/env.example

%changelog
* $(date -u '+%a %b %d %Y') Robby Aprianto <robprian@users.noreply.github.com> - $VERSION-1
- Production release build
EOF

rpmbuild \
  --define "_topdir $TOP" \
  --define "_build_id_links none" \
  --target "$RPMARCH" \
  -bb "$SPEC" >/dev/null 2>"$TOP/rpmbuild.err" || true

BUILT="$(find "$TOP/RPMS" -name '*.rpm' | head -1 || true)"

# Cross-arch on a foreign host (e.g. building aarch64 RPM on x86_64 Debian):
# fall back to an emulated container with matching platform when available.
if [ -z "$BUILT" ] && command -v docker >/dev/null 2>&1; then
  case "$RPMARCH" in
    x86_64)   PLATFORM=linux/amd64 ;;
    aarch64)  PLATFORM=linux/arm64 ;;
    *)        PLATFORM="" ;;
  esac
  if [ -n "$PLATFORM" ]; then
    echo "  ○ Host rpmbuild cannot target $RPMARCH — trying docker ($PLATFORM) …"
    # Prefer a pre-warmed builder image with rpm-build installed; fall back
    # to stock rockylinux:9 with a one-time dnf install.
    BUILDER_IMG="rockylinux:9"
    BUILDER_CMD='dnf install -y rpm-build >/dev/null && '
    if docker image inspect tdocs-rpmbuilder:9 >/dev/null 2>&1; then
      BUILDER_IMG="tdocs-rpmbuilder:9"
      BUILDER_CMD=''
    fi
    docker run --rm --platform "$PLATFORM" \
      -v "$TOP:/top" -w /top \
      "$BUILDER_IMG" \
      bash -c "${BUILDER_CMD}rpmbuild --define \"_topdir /top\" --define \"_build_id_links none\" -bb /top/SPECS/tdocs.spec" \
      >/dev/null || true
    BUILT="$(find "$TOP/RPMS" -name '*.rpm' | head -1 || true)"
  fi
fi

if [ -z "$BUILT" ]; then
  echo "rpmbuild produced no RPM for $RPMARCH" >&2
  cat "$TOP/rpmbuild.err" 2>/dev/null || true
  exit 1
fi
# Canonical name: tdocs_<version>_<arch>.rpm
NAME="tdocs_${VERSION}_${RPMARCH}.rpm"
mkdir -p "$OUTDIR"
cp "$BUILT" "$OUTDIR/$NAME"
echo "==> $OUTDIR/$NAME"
