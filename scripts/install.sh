#!/usr/bin/env bash
# Install the latest (or pinned) wrk3 release from GitHub.
# No Go toolchain required — downloads a prebuilt binary + verifies sha256.
#
#   curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash -s -- --version v0.2.0
#   curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash -s -- --prefix ~/.local
#
# To update an existing install without re-running this script:
#   wrk3 update
set -euo pipefail

REPO="mytmlt/wrk3"
BIN="wrk3"
VERSION="${WRK3_VERSION:-latest}"
PREFIX="${PREFIX:-/usr/local}"
BINDIR="${BINDIR:-$PREFIX/bin}"
VERIFY="${WRK3_VERIFY:-1}"

usage() {
  cat <<EOF
Usage: install.sh [--version vX.Y.Z|latest] [--prefix DIR] [--bindir DIR] [--no-verify]

Installs $BIN from github.com/$REPO releases (prebuilt binary, no Go needed).
Defaults: --version $VERSION --prefix $PREFIX --bindir $BINDIR
Env overrides: WRK3_VERSION, PREFIX, BINDIR, WRK3_VERIFY=0
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      [ $# -ge 2 ] || { echo "--version needs a value" >&2; exit 1; }
      VERSION="$2"; shift 2 ;;
    --prefix)
      [ $# -ge 2 ] || { echo "--prefix needs a value" >&2; exit 1; }
      PREFIX="$2"; BINDIR="$PREFIX/bin"; shift 2 ;;
    --bindir)
      [ $# -ge 2 ] || { echo "--bindir needs a value" >&2; exit 1; }
      BINDIR="$2"; shift 2 ;;
    --no-verify) VERIFY="0"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown flag: $1" >&2; usage; exit 1 ;;
  esac
done

# Normalize: "0.2.0" -> "v0.2.0"; leave "latest" alone.
case "$VERSION" in
  latest) ;;
  v*|V*) VERSION="v${VERSION#?}" ;;
  *) VERSION="v$VERSION" ;;
esac

# download URL -> file (curl preferred, wget fallback).
download() {
  _url="$1"; _out="$2"
  if command -v curl >/dev/null 2>&1; then
    if [ -n "${GITHUB_TOKEN:-}" ]; then
      curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" "$_url" -o "$_out"
    else
      curl -fsSL "$_url" -o "$_out"
    fi
  elif command -v wget >/dev/null 2>&1; then
    if [ -n "${GITHUB_TOKEN:-}" ]; then
      wget -q --header="Authorization: Bearer $GITHUB_TOKEN" -O "$_out" "$_url"
    else
      wget -q -O "$_out" "$_url"
    fi
  else
    echo "curl or wget is required to download releases" >&2
    return 1
  fi
}

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported arch: $arch (supported: amd64, arm64)" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) os="windows" ;;
  *) echo "unsupported os: $os (supported: linux, darwin, windows)" >&2; exit 1 ;;
esac
if [ "$os" = "windows" ] && [ "$arch" = "arm64" ]; then
  echo "no release asset is built for windows/arm64" >&2
  echo "see https://github.com/$REPO/releases (or install WSL2 + the linux asset)" >&2
  exit 1
fi

if [ "$VERSION" = "latest" ]; then
  if command -v curl >/dev/null 2>&1; then
    VERSION="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
  elif command -v wget >/dev/null 2>&1; then
    VERSION="$(wget -Sq -O /dev/null "https://github.com/$REPO/releases/latest" 2>&1 | grep -i 'location:' | tail -1 | sed 's#.*/tag/##;s#[^v0-9A-Za-z._-]*##g')"
  else
    echo "curl or wget is required to resolve the latest release" >&2
    exit 1
  fi
fi
[ -n "$VERSION" ] || { echo "could not resolve latest release" >&2; exit 1; }

ext="tar.gz"
[ "$os" = "windows" ] && ext="zip"
asset="${BIN}_${VERSION}_${os}_${arch}.${ext}"
url="https://github.com/$REPO/releases/download/${VERSION}/${asset}"
checksum_url="https://github.com/$REPO/releases/download/${VERSION}/checksums.txt"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "installing $BIN $VERSION ($os/$arch) to $BINDIR"
if ! download "$url" "$tmp/pkg.$ext"; then
  echo "no release asset at $url" >&2
  echo "see https://github.com/$REPO/releases for available versions" >&2
  exit 1
fi

if [ "$VERIFY" = "1" ]; then
  if download "$checksum_url" "$tmp/checksums.txt" 2>/dev/null; then
    (cd "$tmp" && grep " $asset\$" checksums.txt > asset.sha 2>/dev/null || true)
    if [ -s "$tmp/asset.sha" ]; then
      if command -v sha256sum >/dev/null 2>&1; then
        (cd "$tmp" && mv "pkg.$ext" "$asset" && sha256sum -c asset.sha)
      elif command -v shasum >/dev/null 2>&1; then
        (cd "$tmp" && mv "pkg.$ext" "$asset" && shasum -a 256 -c asset.sha)
      else
        echo "warning: no sha256 tool (sha256sum/shasum), skipping checksum verification" >&2
      fi
    else
      echo "warning: checksum entry for $asset not found, skipping verification" >&2
    fi
  else
    echo "warning: could not download checksums.txt, skipping verification" >&2
  fi
fi

case "$ext" in
  tar.gz) tar -xzf "$tmp/pkg.$ext" -C "$tmp" ;;
  zip)
    command -v unzip >/dev/null 2>&1 || { echo "unzip is required to install the windows asset" >&2; exit 1; }
    unzip -q -o "$tmp/pkg.$ext" -d "$tmp" ;;
esac
[ -f "$tmp/$BIN" ] || [ -f "$tmp/$BIN.exe" ] || { echo "binary $BIN not found in $asset" >&2; exit 1; }
# Normalize windows exe name.
[ -f "$tmp/$BIN.exe" ] && [ ! -f "$tmp/$BIN" ] && BIN_SRC="$tmp/$BIN.exe" || BIN_SRC="$tmp/$BIN"

if ! mkdir -p "$BINDIR" 2>/dev/null; then
  echo "cannot write to $BINDIR (permission denied)" >&2
  echo "re-run with sudo, or install without root: bash install.sh --prefix ~/.local" >&2
  exit 1
fi
if ! install -m 0755 "$BIN_SRC" "$BINDIR/$BIN" 2>/dev/null; then
  echo "cannot write to $BINDIR/$BIN (permission denied)" >&2
  echo "re-run with sudo, or install without root: bash install.sh --prefix ~/.local" >&2
  exit 1
fi
echo "installed $BINDIR/$BIN"
"$BINDIR/$BIN" version
case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) echo "note: $BINDIR is not on PATH; add: export PATH=\"$BINDIR:\$PATH\"" >&2 ;;
esac
