#!/usr/bin/env bash
# Install the latest (or pinned) wrk3 release from GitHub.
#
#   curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash -s -- --version v0.1.0
#   curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash -s -- --prefix ~/.local
#
# Falls back to `go install` when no release asset matches.
set -euo pipefail

REPO="mytmlt/wrk3"
BIN="wrk3"
VERSION="${WRK3_VERSION:-latest}"
PREFIX="${PREFIX:-/usr/local}"
BINDIR="$PREFIX/bin"

usage() {
  cat <<EOF
Usage: install.sh [--version vX.Y.Z|latest] [--prefix DIR]

Installs $BIN from github.com/$REPO releases.
Defaults: --version $VERSION --prefix $PREFIX
Env overrides: WRK3_VERSION, PREFIX
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --prefix)  PREFIX="$2"; BINDIR="$PREFIX/bin"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown flag: $1" >&2; usage; exit 1 ;;
  esac
done

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) os="windows" ;;
  *) echo "unsupported os: $os" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  if command -v curl >/dev/null 2>&1; then
    VERSION="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
  else
    echo "curl is required to resolve the latest release" >&2
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
if curl -fsSL "$url" -o "$tmp/pkg.$ext"; then
  if curl -fsSL "$checksum_url" -o "$tmp/checksums.txt" 2>/dev/null; then
    (cd "$tmp" && grep " $asset\$" checksums.txt > asset.sha 2>/dev/null || true)
    if [ -s "$tmp/asset.sha" ]; then
      if command -v sha256sum >/dev/null 2>&1; then
        (cd "$tmp" && mv "pkg.$ext" "$asset" && sha256sum -c asset.sha)
      elif command -v shasum >/dev/null 2>&1; then
        (cd "$tmp" && mv "pkg.$ext" "$asset" && shasum -a 256 -c asset.sha)
      else
        echo "warning: no sha256 tool, skipping checksum verification" >&2
      fi
    fi
  fi
  case "$ext" in
    tar.gz) tar -xzf "$tmp/pkg.$ext" -C "$tmp" ;;
    zip) unzip -q -o "$tmp/pkg.$ext" -d "$tmp" ;;
  esac
  mkdir -p "$BINDIR"
  install -m 0755 "$tmp/$BIN" "$BINDIR/$BIN"
  echo "installed $BINDIR/$BIN"
  "$BINDIR/$BIN" version
  exit 0
fi

echo "no release asset at $url — falling back to go install" >&2
if ! command -v go >/dev/null 2>&1; then
  echo "go toolchain not found; install Go >= 1.26 or wait for a release asset" >&2
  exit 1
fi
GOBIN="$BINDIR" go install "github.com/$REPO@$VERSION"
# go install names the binary after the module (wrk3), which already
# matches $BIN, so no normalization is needed.
"$BINDIR/$BIN" version
