#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:?usage: build_windows_binary.sh VERSION OUTPUT_PATH [ARCH]}"
OUTPUT_PATH="${2:?usage: build_windows_binary.sh VERSION OUTPUT_PATH [ARCH]}"
ARCH="${3:-amd64}"
if [[ "$ARCH" != amd64 && "$ARCH" != arm64 ]]; then
  echo "Unsupported Windows architecture: $ARCH (supported: amd64, arm64)" >&2
  exit 1
fi
RESOURCE="$ROOT_DIR/cmd/duckchat/duckchat_windows_${ARCH}.syso"
RESOURCE_PREFIX="${RESOURCE%_windows_${ARCH}.syso}"

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
  --arch "$ARCH" \
  --icon "$ROOT_DIR/docs/images/logo.png" \
  --out "$RESOURCE_PREFIX" \
  --file-description "DuckDuckGo Chat CLI" \
  --product-name "DuckDuckGo Chat CLI"
test -s "$RESOURCE"

mkdir -p "$(dirname -- "$OUTPUT_PATH")"
LDFLAGS="-X main.Version=v$VERSION -X duckduckgo-chat-cli/internal/version.Current=$VERSION"
GOOS=windows GOARCH="$ARCH" go build \
  -ldflags "$LDFLAGS" \
  -o "$OUTPUT_PATH" \
  "$ROOT_DIR/cmd/duckchat"
