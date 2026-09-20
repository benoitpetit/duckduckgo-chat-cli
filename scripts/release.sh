#!/bin/bash

# Automated release script for DuckDuckGo Chat CLI
# Usage: ./scripts/release.sh [version]

set -e

# Colors for messages
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Utility functions
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

# Preliminary checks
check_git_status() {
    if [ -n "$(git status --porcelain)" ]; then
        log_error "Working directory is not clean. Commit or stash your changes."
        exit 1
    fi
    log_success "Working directory is clean"
}

check_git_branch() {
    CURRENT_BRANCH=$(git branch --show-current)
    if [ "$CURRENT_BRANCH" != "master" ]; then
        log_warning "You are not on master branch (currently on: $CURRENT_BRANCH)"
        echo -n "Continue anyway? (y/n): "
        read -r CONFIRM
        if [[ $CONFIRM != "y" && $CONFIRM != "Y" ]]; then
            log_info "Release cancelled"
            exit 0
        fi
    fi
    log_success "Branch verified: $CURRENT_BRANCH"
}

get_version() {
    if [ -n "$1" ]; then
        VERSION="$1"
    else
        # Get latest version and suggest incrementation
        LAST_TAG=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo "1.1.5")
        log_info "Latest version detected: v$LAST_TAG"
        
        IFS='.' read -r -a VERSION_PARTS <<< "$LAST_TAG"
        MAJOR=${VERSION_PARTS[0]}
        MINOR=${VERSION_PARTS[1]}
        PATCH=${VERSION_PARTS[2]}
        
        NEXT_PATCH=$((PATCH + 1))
        NEXT_MINOR=$((MINOR + 1))
        NEXT_MAJOR=$((MAJOR + 1))
        
        echo -e "\n${BLUE}Choose release type:${NC}"
        echo "1) Patch: v${MAJOR}.${MINOR}.${NEXT_PATCH} (bug fixes)"
        echo "2) Minor: v${MAJOR}.${NEXT_MINOR}.0 (new features)"
        echo "3) Major: v${NEXT_MAJOR}.0.0 (breaking changes)"
        echo "4) Custom: specify manually"
        echo -n "Your choice (1-4): "
        read -r CHOICE
        
        case $CHOICE in
            1) VERSION="${MAJOR}.${MINOR}.${NEXT_PATCH}" ;;
            2) VERSION="${MAJOR}.${NEXT_MINOR}.0" ;;
            3) VERSION="${NEXT_MAJOR}.0.0" ;;
            4) 
                echo -n "Enter version (format X.X.X): "
                read -r VERSION
                ;;
            *) 
                log_error "Invalid choice"
                exit 1
                ;;
        esac
    fi
    
    # Format validation
    if ! [[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        log_error "Invalid version format: $VERSION"
        log_info "Use format X.X.X (e.g., 1.2.0)"
        exit 1
    fi
    
    # Check that version doesn't exist
    if git rev-parse "v$VERSION" >/dev/null 2>&1; then
        log_error "Version v$VERSION already exists!"
        exit 1
    fi
    
    log_success "Selected version: v$VERSION"
}

create_release_branch() {
    RELEASE_BRANCH="release/v$VERSION"
    
    log_info "Creating release branch: $RELEASE_BRANCH"
    git checkout -b "$RELEASE_BRANCH"
    
    log_success "Branch $RELEASE_BRANCH created"
}

push_to_github() {
    RELEASE_BRANCH="release/v$VERSION"
    
    log_info "Pushing branch to GitHub..."
    git push origin "$RELEASE_BRANCH"
    
    log_success "Branch pushed to GitHub"
    
    # Instructions for creating PR
    echo -e "\n${BLUE}📋 Next steps:${NC}"
    echo "1. Go to GitHub: https://github.com/benoitpetit/duckduckgo-chat-cli"
    echo "2. Create a Pull Request from '$RELEASE_BRANCH' to 'prod'"
    echo "3. Once merged, release v$VERSION will be automatically created"
    echo ""
    echo -e "${YELLOW}Or use this direct URL:${NC}"
    echo "https://github.com/benoitpetit/duckduckgo-chat-cli/compare/prod...$RELEASE_BRANCH?quick_pull=1"
}

create_prod_branch_if_needed() {
    # Check if prod branch exists
    if ! git show-ref --verify --quiet refs/heads/prod; then
        log_info "Creating 'prod' branch (first time)"
        git checkout -b prod
        git push -u origin prod
        git checkout master
        log_success "'prod' branch created and configured"
    else
        log_success "'prod' branch already exists"
    fi
    
    # Check if prod branch exists on remote
    if ! git show-ref --verify --quiet refs/remotes/origin/prod; then
        log_info "Pushing 'prod' branch to remote"
        git push -u origin prod
        log_success "'prod' branch available on GitHub"
    fi
}

show_help() {
    echo -e "${BLUE}🚀 DuckDuckGo Chat CLI Release Script${NC}"
    echo ""
    echo "Usage:"
    echo "  $0 [version]"
    echo ""
    echo "Examples:"
    echo "  $0                    # Interactive mode"
    echo "  $0 1.2.0             # Specific version"
    echo ""
    echo "The script will:"
    echo "  1. Check Git repository status"
    echo "  2. Create a release branch"
    echo "  3. Push it to GitHub"
    echo "  4. Give you instructions to create the PR"
    echo ""
    echo "Options:"
    echo "  -h, --help           Show this help"
}

# Main
main() {
    echo -e "${BLUE}🚀 DuckDuckGo Chat CLI Release Script${NC}"
    echo ""
    
    # Checks
    check_git_status
    check_git_branch
    create_prod_branch_if_needed
    
    # Version management
    get_version "$1"
    
    # Final confirmation
    echo -e "\n${YELLOW}📋 Release summary:${NC}"
    echo "  Version: v$VERSION"
    echo "  Current branch: $(git branch --show-current)"
    echo "  Latest version: $(git describe --tags --abbrev=0 2>/dev/null || echo 'none')"
    echo ""
    echo -n "Proceed with release creation? (y/n): "
    read -r CONFIRM
    
    if [[ $CONFIRM != "y" && $CONFIRM != "Y" ]]; then
        log_info "Release cancelled"
        exit 0
    fi
    
    # Execution
    create_release_branch
    push_to_github
    
    echo -e "\n${GREEN}🎉 Release v$VERSION initiated successfully!${NC}"
}

# Handle arguments
case "${1:-}" in
    -h|--help)
        show_help
        exit 0
        ;;
    *)
        main "$1"
        ;;
esac
