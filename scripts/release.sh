#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  read -r -p "Release version (e.g. 1.6.1): " VERSION
fi
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Invalid version. Use X.Y.Z, for example 1.6.1." >&2
  exit 1
fi
if ! command -v gh >/dev/null 2>&1; then
  echo "GitHub CLI (gh) is required to dispatch a release." >&2
  exit 1
fi
if ! gh auth status >/dev/null 2>&1; then
  echo "Authenticate with GitHub CLI first: gh auth login" >&2
  exit 1
fi
if [[ -n "$(git status --porcelain)" ]]; then
  echo "Commit or stash changes before dispatching a release." >&2
  exit 1
fi

git fetch origin master --quiet
if [[ "$(git rev-parse HEAD)" != "$(git rev-parse origin/master)" ]]; then
  echo "The current commit is not the pushed origin/master commit. Push master first." >&2
  exit 1
fi
if git ls-remote --exit-code --tags origin "refs/tags/v$VERSION" >/dev/null 2>&1; then
  echo "Version v$VERSION already has a tag on origin." >&2
  exit 1
fi

gh workflow run release.yml --ref master -f "version=$VERSION"
echo "Dispatched the release workflow for v$VERSION. Monitor it with: gh run watch"
