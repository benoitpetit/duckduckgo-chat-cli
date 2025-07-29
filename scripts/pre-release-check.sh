#!/bin/bash

# Pre-release verification script
# Checks that the project is ready for a release

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}ℹ️  $1${NC}"; }
log_success() { echo -e "${GREEN}✅ $1${NC}"; }
log_warning() { echo -e "${YELLOW}⚠️  $1${NC}"; }
log_error() { echo -e "${RED}❌ $1${NC}"; }

CHECKS_PASSED=0
CHECKS_TOTAL=0

check() {
    CHECKS_TOTAL=$((CHECKS_TOTAL + 1))
    if eval "$1"; then
        log_success "$2"
        CHECKS_PASSED=$((CHECKS_PASSED + 1))
        return 0
    else
        log_error "$2"
        return 1
    fi
}

echo -e "${BLUE}🔍 Pre-release checks for DuckDuckGo Chat CLI${NC}"
echo ""

# Go environment verification
log_info "Checking Go environment..."
check "go version >/dev/null 2>&1" "Go is installed and accessible"
check "go version | grep -q '1\.\(2[1-9]\|[3-9][0-9]\)'" "Go version >= 1.21"

# Dependencies verification
log_info "Checking dependencies..."
check "go mod verify" "Go modules verified"
check "go mod tidy -diff || (go mod tidy && false)" "go.mod and go.sum up to date"

# Code verification
log_info "Checking code..."
check "gofmt -l . | wc -l | grep -q '^0$'" "Code formatted with gofmt"
check "go vet ./..." "go vet passed"

# Tests verification
log_info "Checking tests..."
if find . -name "*_test.go" -type f | grep -q .; then
    check "go test ./..." "Tests passed"
else
    log_warning "No test files found (check skipped)"
    CHECKS_TOTAL=$((CHECKS_TOTAL + 1))
    CHECKS_PASSED=$((CHECKS_PASSED + 1))
fi

# Cross-platform builds verification
log_info "Checking cross-platform builds..."
check "GOOS=linux GOARCH=amd64 go build -o /tmp/test_linux ./cmd/duckchat/main.go && rm -f /tmp/test_linux" "Linux AMD64 build"
check "GOOS=windows GOARCH=amd64 go build -o /tmp/test_windows.exe ./cmd/duckchat/main.go && rm -f /tmp/test_windows.exe" "Windows AMD64 build"
check "GOOS=darwin GOARCH=arm64 go build -o /tmp/test_darwin ./cmd/duckchat/main.go && rm -f /tmp/test_darwin" "Darwin ARM64 build"
check "GOOS=darwin GOARCH=amd64 go build -o /tmp/test_darwin_amd64 ./cmd/duckchat/main.go && rm -f /tmp/test_darwin_amd64" "Darwin AMD64 build"

# Git verification
log_info "Checking Git..."
if [ -z "$(git status --porcelain)" ]; then
    check "true" "Working directory is clean"
else
    log_warning "Working directory has uncommitted changes"
    echo -n "Continue anyway? (y/n): "
    read -r CONFIRM
    if [[ $CONFIRM == "y" || $CONFIRM == "Y" ]]; then
        CHECKS_TOTAL=$((CHECKS_TOTAL + 1))
        CHECKS_PASSED=$((CHECKS_PASSED + 1))
        log_info "Continuing with uncommitted changes..."
    else
        check "false" "Working directory is clean"
    fi
fi
check "git remote get-url origin | grep -q 'duckduckGO-chat-cli'" "Origin remote configured"

# Important files verification
log_info "Checking files..."
check "[ -f README.md ]" "README.md present"
check "[ -f go.mod ]" "go.mod present"
check "[ -f cmd/duckchat/main.go ]" "Main entry point present"
check "[ -f scripts/build.sh ]" "Build script present"
check "[ -f .github/workflows/release.yml ]" "Release workflow present"

# Documentation verification
log_info "Checking documentation..."
check "grep -q 'v[0-9]\+\.[0-9]\+\.[0-9]\+' README.md" "Version mentioned in README.md"
check "[ -f .github/RELEASE_WORKFLOW.md ]" "Workflow documentation present"

# API Documentation verification
log_info "Checking API documentation..."
if command -v swag >/dev/null 2>&1; then
    # Check if documentation can be generated without errors
    check "swag init --generalInfo internal/api/docs.go --output /tmp/docs_test --parseInternal >/dev/null 2>&1 && rm -rf /tmp/docs_test" "API documentation generates without errors"
    
    # Check if current documentation is up-to-date
    if [ -f "docs/docs.go" ] && [ -f "docs/swagger.json" ] && [ -f "docs/swagger.yaml" ]; then
        # Generate fresh docs in temp directory and compare
        mkdir -p /tmp/docs_check
        swag init --generalInfo internal/api/docs.go --output /tmp/docs_check --parseInternal >/dev/null 2>&1
        
        if cmp -s "docs/swagger.json" "/tmp/docs_check/swagger.json"; then
            check "true" "API documentation is up-to-date"
        else
            log_warning "API documentation may be outdated. Run: ./scripts/generate-docs.sh"
            check "false" "API documentation is up-to-date"
        fi
        
        rm -rf /tmp/docs_check
    else
        check "false" "API documentation files present"
    fi
else
    log_warning "swag not installed. Install with: go install github.com/swaggo/swag/cmd/swag@latest"
    CHECKS_TOTAL=$((CHECKS_TOTAL + 1))
    CHECKS_PASSED=$((CHECKS_PASSED + 1))
fi

# Optional check: golangci-lint
if command -v golangci-lint >/dev/null 2>&1; then
    log_info "Checking with golangci-lint..."
    check "golangci-lint run" "golangci-lint passed"
else
    log_warning "golangci-lint not installed (optional check)"
fi

# Summary
echo ""
echo -e "${BLUE}📊 Checks summary:${NC}"
echo "  Passed: $CHECKS_PASSED/$CHECKS_TOTAL"

if [ $CHECKS_PASSED -eq $CHECKS_TOTAL ]; then
    echo -e "\n${GREEN}🎉 All checks passed!${NC}"
    echo -e "${GREEN}The project is ready for release.${NC}"
    exit 0
else
    FAILED=$((CHECKS_TOTAL - CHECKS_PASSED))
    echo -e "\n${RED}⚠️  $FAILED check(s) failed${NC}"
    echo -e "${RED}Fix the issues before proceeding with release.${NC}"
    exit 1
fi
