#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf -- "$TEMP_DIR"' EXIT

fail() {
	echo "❌ $*" >&2
	exit 1
}

mkdir -p "$TEMP_DIR/docs"
go run github.com/swaggo/swag/cmd/swag@v1.16.6 init \
	--generalInfo internal/api/docs.go \
	--output "$TEMP_DIR/docs" \
	--parseInternal >/dev/null

cmp -s docs/docs.go "$TEMP_DIR/docs/docs.go" || \
	fail "docs/docs.go is out of date; run ./scripts/generate-docs.sh"
cmp -s docs/swagger.yaml "$TEMP_DIR/docs/swagger.yaml" || \
	fail "docs/swagger.yaml is out of date; run ./scripts/generate-docs.sh"
# swag omits the final newline from swagger.json; normalize it for comparison.
cmp -s <(awk '1' docs/swagger.json) <(awk '1' "$TEMP_DIR/docs/swagger.json") || \
	fail "docs/swagger.json is out of date; run ./scripts/generate-docs.sh"

echo "✅ Generated API documentation is up to date."
