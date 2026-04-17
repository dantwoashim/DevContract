#!/usr/bin/env bash
# DevContract installer for macOS and Linux.
# Usage:
#   ./scripts/install.sh --version v1.2.3 --install-dir /tmp/bin
#   DEVCONTRACT_VERSION=v1.2.3 ./scripts/install.sh

set -euo pipefail

REPO="${DEVCONTRACT_INSTALL_REPO:-dantwoashim/devcontract}"
INSTALL_DIR="${DEVCONTRACT_INSTALL_DIR:-/usr/local/bin}"
VERSION="${DEVCONTRACT_VERSION:-latest}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="$2"
      shift 2
      ;;
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --repo)
      REPO="$2"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS (use install.ps1 for Windows)" >&2; exit 1 ;;
esac

echo "Installing DevContract for ${OS}/${ARCH}"

api_url="https://api.github.com/repos/${REPO}/releases"
if [[ "$VERSION" == "latest" ]]; then
  release_json="$(curl -fsSL "${api_url}/latest")"
  VERSION="v$(printf '%s' "$release_json" | python -c "import json,sys; print(json.load(sys.stdin)['tag_name'].lstrip('v'))")"
else
  VERSION="v${VERSION#v}"
fi

echo "Version: ${VERSION}"

archive="devcontract_${VERSION#v}_${OS}_${ARCH}.tar.gz"
archive_url="https://github.com/${REPO}/releases/download/${VERSION}/${archive}"
checksums_url="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${archive}"
curl -fsSL "$archive_url" -o "${tmp}/${archive}"
curl -fsSL "$checksums_url" -o "${tmp}/checksums.txt"

expected="$(grep " ${archive}\$" "${tmp}/checksums.txt" | awk '{print $1}')"
if [[ -z "$expected" ]]; then
  echo "Checksum for ${archive} not found in checksums.txt" >&2
  exit 1
fi

actual="$(python - "${tmp}/${archive}" <<'PY'
import hashlib
import pathlib
import sys
path = pathlib.Path(sys.argv[1])
print(hashlib.sha256(path.read_bytes()).hexdigest())
PY
)"

if [[ "$actual" != "$expected" ]]; then
  echo "Checksum verification failed for ${archive}" >&2
  echo "Expected: ${expected}" >&2
  echo "Actual:   ${actual}" >&2
  exit 1
fi

echo "Checksum verified"
tar -xzf "${tmp}/${archive}" -C "$tmp"

mkdir -p "$INSTALL_DIR"
target="${INSTALL_DIR}/devcontract"
if [[ -w "$INSTALL_DIR" ]]; then
  mv "${tmp}/devcontract" "$target"
else
  echo "Installing to ${INSTALL_DIR} requires sudo"
  sudo mv "${tmp}/devcontract" "$target"
fi
chmod +x "$target"

echo "Installed devcontract ${VERSION} to ${target}"
