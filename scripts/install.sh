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
# Extra args are appended to the downloader (e.g. -H "Accept: ...").
# NOTE: no auth is sent on browser download URLs — github.com web URLs
# don't accept Bearer tokens (private repos go through the API instead;
# see fetch_asset).
download() {
  _url="$1"; _out="$2"; shift 2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$@" "$_url" -o "$_out"
  elif command -v wget >/dev/null 2>&1; then
    _headers=""
    _next_is_header=0
    for _a in "$@"; do
      if [ "$_next_is_header" = "1" ]; then
        _headers="$_headers --header=$_a"
        _next_is_header=0
      elif [ "$_a" = "-H" ]; then
        _next_is_header=1
      fi
    done
    # shellcheck disable=SC2086
    wget -q $_headers -O "$_out" "$_url"
  else
    echo "curl or wget is required to download releases" >&2
    return 1
  fi
}

# AUTH_HDR holds the curl-style auth header args (empty when no token).
# Supports GITHUB_TOKEN and GH_TOKEN (gh CLI convention).
AUTH_HDR=()
if [ -n "${GITHUB_TOKEN:-}" ]; then
  AUTH_HDR=(-H "Authorization: Bearer $GITHUB_TOKEN")
elif [ -n "${GH_TOKEN:-}" ]; then
  AUTH_HDR=(-H "Authorization: Bearer $GH_TOKEN")
fi

# api_download fetches a release asset by name through the GitHub API.
# Needed for private repos (browser URLs 404 there even with a token).
api_download() {
  _name="$1"; _out="$2"
  _tag="$VERSION"
  _api="https://api.github.com/repos/$REPO/releases/tags/$_tag"
  if command -v python3 >/dev/null 2>&1; then
    download "$_api" "$tmp/release.json" "${AUTH_HDR[@]}" -H "Accept: application/vnd.github+json" || return 1
    _id="$(python3 -c '
import json, sys
with open(sys.argv[1]) as f:
    rel = json.load(f)
for a in rel.get("assets", []):
    if a.get("name") == sys.argv[2]:
        print(a["id"])
        break
else:
    sys.exit(1)
' "$tmp/release.json" "$_name")" || return 1
  elif command -v jq >/dev/null 2>&1; then
    download "$_api" "$tmp/release.json" "${AUTH_HDR[@]}" -H "Accept: application/vnd.github+json" || return 1
    _id="$(jq -r --arg n "$_name" '.assets[] | select(.name == $n) | .id' "$tmp/release.json" | grep -E '^[0-9]+$')" || return 1
  else
    echo "python3 or jq is required to install from a private repo (or install gh)" >&2
    return 1
  fi
  [ -n "$_id" ] || return 1
  _api="https://api.github.com/repos/$REPO/releases/assets/$_id"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${AUTH_HDR[@]}" -H "Accept: application/octet-stream" "$_api" -o "$_out"
  elif command -v wget >/dev/null 2>&1; then
    _auth_header=""
    [ "${#AUTH_HDR[@]}" -eq 2 ] && _auth_header="--header=${AUTH_HDR[1]}"
    # shellcheck disable=SC2086
    wget -q $_auth_header --header="Accept: application/octet-stream" -O "$_out" "$_api"
  else
    return 1
  fi
}

# fetch_asset downloads a release file, trying in order:
#   1. browser download URL (public repos, no auth needed)
#   2. `gh release download` (private repos, uses gh auth)
#   3. GitHub API + token (private repos, GITHUB_TOKEN/GH_TOKEN)
fetch_asset() {
  _name="$1"; _out="$2"; _url="$3"
  if download "$_url" "$_out" 2>/dev/null; then
    return 0
  fi
  if command -v gh >/dev/null 2>&1; then
    if gh release download "$VERSION" -R "$REPO" -p "$_name" -D "$tmp/gh-dl" >/dev/null 2>&1; then
      mv "$tmp/gh-dl/$_name" "$_out"
      return 0
    fi
  fi
  if [ -n "${GITHUB_TOKEN:-}" ] || [ -n "${GH_TOKEN:-}" ]; then
    if api_download "$_name" "$_out" 2>/dev/null; then
      return 0
    fi
  fi
  return 1
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
# NOTE: GoReleaser strips the "v" in asset filenames (tag v0.3.0 ->
# file wrk3_0.3.0_...), while tag/URLs keep it.
asset_version="${VERSION#v}"
asset_version="${asset_version#V}"
asset="${BIN}_${asset_version}_${os}_${arch}.${ext}"
url="https://github.com/$REPO/releases/download/${VERSION}/${asset}"
checksum_url="https://github.com/$REPO/releases/download/${VERSION}/checksums.txt"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "installing $BIN $VERSION ($os/$arch) to $BINDIR"
if ! fetch_asset "$asset" "$tmp/pkg.$ext" "$url"; then
  echo "no release asset at $url" >&2
  if [ -z "${GITHUB_TOKEN:-}" ] && [ -z "${GH_TOKEN:-}" ] && ! command -v gh >/dev/null 2>&1; then
    echo "if this is a private repo, set GITHUB_TOKEN (or install gh) and retry" >&2
  fi
  echo "see https://github.com/$REPO/releases for available versions" >&2
  exit 1
fi

if [ "$VERIFY" = "1" ]; then
  if fetch_asset "checksums.txt" "$tmp/checksums.txt" "$checksum_url" 2>/dev/null; then
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

# The verified package may have been renamed to $asset above.
PKG="$tmp/pkg.$ext"
[ -f "$tmp/$asset" ] && PKG="$tmp/$asset"

case "$ext" in
  tar.gz) tar -xzf "$PKG" -C "$tmp" ;;
  zip)
    command -v unzip >/dev/null 2>&1 || { echo "unzip is required to install the windows asset" >&2; exit 1; }
    unzip -q -o "$PKG" -d "$tmp" ;;
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
