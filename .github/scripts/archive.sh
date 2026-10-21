#!/usr/bin/env bash
# Usage: archive.sh <binary> <name> <goos>
# Packs the binary with README.md and LICENSE into dist/<name>.zip (windows)
# or dist/<name>.tar.gz (others), inside a top-level <name>/ folder.
set -euo pipefail

bin=$1 name=$2 goos=$3
mkdir -p "dist/$name"
cp "$bin" README.md LICENSE "dist/$name/"
cd dist
if [ "$goos" = windows ]; then
  if command -v zip >/dev/null; then
    zip -qr "$name.zip" "$name"
  else
    7z a -tzip -bso0 "$name.zip" "$name"
  fi
else
  tar -czf "$name.tar.gz" "$name"
fi
rm -rf "$name"
