#!/bin/bash

# API Documentation Generation Script
# Generates Swagger documentation from annotations

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

log_info "Generating API documentation..."

# Check if swag is installed
if ! command -v swag &> /dev/null; then
    log_warning "swag command not found. Installing..."
    go install github.com/swaggo/swag/cmd/swag@v1.16.6
    log_success "swag installed successfully"
fi

# Check if source file exists
SOURCE_FILE="internal/api/docs.go"
if [ ! -f "$SOURCE_FILE" ]; then
    log_error "Source file $SOURCE_FILE not found"
    exit 1
fi

# Generate documentation
log_info "Running swag init..."
swag init \
    --generalInfo internal/api/docs.go \
    --output docs/ \
    --parseInternal

# Verify generated files
DOCS_DIR="docs"
EXPECTED_FILES=("docs.go" "swagger.json" "swagger.yaml")

for file in "${EXPECTED_FILES[@]}"; do
    if [ -f "$DOCS_DIR/$file" ]; then
        log_success "Generated: $DOCS_DIR/$file"
    else
        log_error "Failed to generate: $DOCS_DIR/$file"
        exit 1
    fi
done

# Show file sizes
log_info "Generated files:"
ls -lh $DOCS_DIR/*.go $DOCS_DIR/*.json $DOCS_DIR/*.yaml 2>/dev/null || true

log_success "API documentation generated successfully!"
log_info "Documentation available at: /doc/index.html when server is running"
