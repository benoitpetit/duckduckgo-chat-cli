#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p "$ROOT_DIR/build/go-cache" "$ROOT_DIR/build/go-tmp"
export GOCACHE="$ROOT_DIR/build/go-cache"
export GOTMPDIR="$ROOT_DIR/build/go-tmp"
export TMPDIR="$ROOT_DIR/build/go-tmp"

exec go run ./cmd/duckchat "$@"
