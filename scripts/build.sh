#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  read -r -p "🔖 Enter version number (e.g. 1.0.0): " VERSION
fi
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "❌ Invalid version format. Please use X.X.X format (e.g. 1.0.0)" >&2
  exit 1
fi

if [[ $# -eq 0 ]]; then
  read -r -p "🤔 Build version $VERSION? (y/n): " CONFIRM
  if [[ ! "$CONFIRM" =~ ^[yY]$ ]]; then
    echo "❌ Build cancelled"
    exit 0
  fi
fi

BUILD_DIR="${BUILD_DIR:-$ROOT_DIR/build}"
mkdir -p "$BUILD_DIR"
find "$BUILD_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +

echo "🚀 Building DuckDuckGo Chat CLI v$VERSION..."
echo "📚 Generating API documentation..."
"$ROOT_DIR/scripts/generate-docs.sh"

LDFLAGS="-X main.Version=v$VERSION -X duckduckgo-chat-cli/internal/version.Current=$VERSION"

echo "📦 Building Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_linux_amd64" ./cmd/duckchat

echo "📦 Building Windows AMD64 with logo.png application icon..."
"$ROOT_DIR/scripts/build_windows_binary.sh" "$VERSION" "$BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_windows_amd64.exe"

echo "📦 Building Darwin ARM64..."
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o "$BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_darwin_arm64" ./cmd/duckchat

echo "📦 Building Darwin AMD64..."
GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_darwin_amd64" ./cmd/duckchat

echo "🔐 Generating SHA256 checksums..."
checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$1"
  else
    shasum -a 256 "$1"
  fi
}
for binary in "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_*; do
  [[ "$binary" == *.sha256 || "$binary" == *.zip ]] && continue
  checksum "$binary" > "$binary.sha256"
done

echo "📚 Creating release archive..."
archive="$BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_release.zip"
zip -j "$archive" "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_linux_amd64 \
  "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_darwin_arm64 \
  "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_darwin_amd64 \
  "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_windows_amd64.exe \
  "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_linux_amd64.sha256 \
  "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_darwin_arm64.sha256 \
  "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_darwin_amd64.sha256 \
  "$BUILD_DIR"/duckduckgo-chat-cli_v"$VERSION"_windows_amd64.exe.sha256

echo "✅ Build v$VERSION complete! Files available in $BUILD_DIR:"
ls -lh "$BUILD_DIR"
