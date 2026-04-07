#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${1:-dist/source}"
VERSION="${2:-$(git rev-parse --short HEAD)}"
PREFIX="envsync-${VERSION}/"

mkdir -p "$OUT_DIR"

git archive --format=tar.gz --prefix "$PREFIX" --output "$OUT_DIR/envsync-${VERSION}-source.tar.gz" HEAD
git archive --format=zip --prefix "$PREFIX" --output "$OUT_DIR/envsync-${VERSION}-source.zip" HEAD

printf '%s\n' "$OUT_DIR/envsync-${VERSION}-source.tar.gz"
printf '%s\n' "$OUT_DIR/envsync-${VERSION}-source.zip"
