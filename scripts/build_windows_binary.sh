#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:?usage: build_windows_binary.sh VERSION OUTPUT_PATH}"
OUTPUT_PATH="${2:?usage: build_windows_binary.sh VERSION OUTPUT_PATH}"
RESOURCE="$ROOT_DIR/cmd/duckchat/duckchat_windows_amd64.syso"
RESOURCE_PREFIX="${RESOURCE%_windows_amd64.syso}"

if [[ ! -f "$ROOT_DIR/docs/images/logo.png" ]]; then
  echo "Windows application icon source not found: $ROOT_DIR/docs/images/logo.png" >&2
  exit 1
fi
if [[ -e "$RESOURCE" ]]; then
  echo "Refusing to overwrite existing Windows resource: $RESOURCE" >&2
  exit 1
fi

cleanup() {
  rm -f -- "$RESOURCE"
}
trap cleanup EXIT

# go-winres accepts PNG input, resizes it to Windows icon sizes, and emits a
# package-local COFF resource consumed by the Go linker.
go run github.com/tc-hib/go-winres@v0.3.1 simply \
  --arch amd64 \
  --icon "$ROOT_DIR/docs/images/logo.png" \
  --out "$RESOURCE_PREFIX" \
  --file-description "DuckDuckGo Chat CLI" \
  --product-name "DuckDuckGo Chat CLI"
test -s "$RESOURCE"

mkdir -p "$(dirname -- "$OUTPUT_PATH")"
LDFLAGS="-X main.Version=v$VERSION -X duckduckgo-chat-cli/internal/version.Current=$VERSION"
GOOS=windows GOARCH=amd64 go build \
  -ldflags "$LDFLAGS" \
  -o "$OUTPUT_PATH" \
  "$ROOT_DIR/cmd/duckchat"
