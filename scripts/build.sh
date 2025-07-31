#!/bin/bash

# Demande de la version
echo -n "🔖 Enter version number (e.g. 1.0.0): "
read VERSION

# Validation du format de version (X.X.X)
if ! [[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "❌ Invalid version format. Please use X.X.X format (e.g. 1.0.0)"
    exit 1
fi

# Confirmation
echo -n "🤔 Build version $VERSION? (y/n): "
read CONFIRM
if [[ $CONFIRM != "y" && $CONFIRM != "Y" ]]; then
    echo "❌ Build cancelled"
    exit 0
fi

# Création du dossier build s'il n'existe pas
BUILD_DIR="build"
mkdir -p $BUILD_DIR

# Nettoyage du dossier build
rm -rf $BUILD_DIR/*

echo "🚀 Building DuckDuckGo Chat CLI v$VERSION..."

# Génération de la documentation API
echo "📚 Generating API documentation..."
./scripts/generate-docs.sh

# Build pour Linux
echo "📦 Building Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=v$VERSION" -o $BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_linux_amd64 ./cmd/duckchat/main.go

# Build pour Windows
echo "📦 Building Windows AMD64..."
GOOS=windows GOARCH=amd64 go build -ldflags "-X main.Version=v$VERSION" -o $BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_windows_amd64.exe ./cmd/duckchat/main.go

# Génération du hash SHA256 pour Windows
echo "🔐 Generating SHA256 hash..."
cd $BUILD_DIR
sha256sum duckduckgo-chat-cli_v${VERSION}_windows_amd64.exe > duckduckgo-chat-cli_v${VERSION}_windows_amd64.exe.sha256
cd ..

# Build pour Apple Silicon
echo "📦 Building Darwin ARM64..."
GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.Version=v$VERSION" -o $BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_darwin_arm64 ./cmd/duckchat/main.go

# Build pour Intel Mac
echo "📦 Building Darwin AMD64..."
GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.Version=v$VERSION" -o $BUILD_DIR/duckduckgo-chat-cli_v${VERSION}_darwin_amd64 ./cmd/duckchat/main.go

# Création du zip de release
echo "📚 Creating release archive..."
cd $BUILD_DIR
zip duckduckgo-chat-cli_v${VERSION}_release.zip \
    duckduckgo-chat-cli_v${VERSION}_linux_amd64 \
    duckduckgo-chat-cli_v${VERSION}_darwin_arm64 \
    duckduckgo-chat-cli_v${VERSION}_darwin_amd64 \
    duckduckgo-chat-cli_v${VERSION}_windows_amd64.exe \
    duckduckgo-chat-cli_v${VERSION}_windows_amd64.exe.sha256
cd ..

echo "✅ Build v$VERSION complete! Files available in $BUILD_DIR:"
ls -lh $BUILD_DIR

# Vérification des fichiers
echo -e "\n🔍 SHA256 hashes:"
cd $BUILD_DIR
sha256sum *
