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

check_go_version() {
  local required current required_major required_minor current_major current_minor
  required="$(awk '$1 == "go" { print $2; exit }' go.mod)"
  current="${GOVERSION:-$(go env GOVERSION)}"
  current="${current#go}"
  IFS=. read -r required_major required_minor _ <<< "$required"
  IFS=. read -r current_major current_minor _ <<< "$current"
  if (( current_major < required_major || (current_major == required_major && current_minor < required_minor) )); then
    fail "Go $required or newer is required; found Go $current"
  fi
  echo "✅ Go $current meets go.mod minimum $required"
}

check_ci_go_versions() {
  local workflow setup_go_steps go_version_files hardcoded_go_versions
  for workflow in .github/workflows/test.yml .github/workflows/release.yml; do
    setup_go_steps="$(grep -c 'uses: actions/setup-go@' "$workflow" || true)"
    go_version_files="$(grep -cE '^[[:space:]]*go-version-file:[[:space:]]*go\.mod[[:space:]]*$' "$workflow" || true)"
    hardcoded_go_versions="$(grep -cE '^[[:space:]]*go-version:' "$workflow" || true)"
    if [[ "$setup_go_steps" -eq 0 || "$setup_go_steps" -ne "$go_version_files" || "$hardcoded_go_versions" -ne 0 ]]; then
      fail "$workflow must select its Go toolchain from go.mod"
    fi
  done
  echo "✅ GitHub Actions use the Go version declared in go.mod"
}

echo "🔍 Pre-release checks for DuckDuckGo Chat CLI"
go version >/dev/null
check_go_version
check_ci_go_versions

printf '\n🔐 Verifying Go modules...\n'
go mod verify
go mod tidy -diff

printf '\n🧹 Checking Go formatting...\n'
unformatted="$(gofmt -s -l .)"
if [[ -n "$unformatted" ]]; then
  printf '%s\n' "$unformatted" >&2
  fail "Go source needs gofmt"
fi

echo "🔎 Running go vet..."
go vet ./...

echo "🧪 Running all Go tests..."
go test ./...

echo "📦 Building the Linux target..."
GOOS=linux GOARCH=amd64 go build -o "$TEMP_DIR/duckchat-linux-amd64" ./cmd/duckchat
echo "ℹ️ macOS binaries require native macOS runners and are built in the release workflow."

echo "🪟 Building Windows executable with docs/images/logo.png..."
"$ROOT_DIR/scripts/build_windows_binary.sh" verify "$TEMP_DIR/duckchat-windows-amd64.exe"
test -s "$TEMP_DIR/duckchat-windows-amd64.exe" || fail "Windows executable was not produced"
if command -v file >/dev/null 2>&1; then
  file "$TEMP_DIR/duckchat-windows-amd64.exe" | grep -q 'PE32+' || fail "Windows output is not a PE32+ executable"
fi

for required_file in README.md docs/BUILD.md go.mod scripts/build.sh scripts/build_windows_binary.sh .github/workflows/release.yml docs/images/logo.png; do
  [[ -s "$required_file" ]] || fail "Required project file is missing or empty: $required_file"
done

grep -q '/dashboard' README.md || fail "README does not document /dashboard"
grep -q 'logo.png' docs/BUILD.md || fail "Build documentation does not mention the Windows icon source"

echo "📚 Checking generated REST API documentation..."
mkdir -p "$TEMP_DIR/docs"
go run github.com/swaggo/swag/cmd/swag@v1.16.6 init \
  --generalInfo internal/api/docs.go \
  --output "$TEMP_DIR/docs" \
  --parseInternal >/dev/null
cmp -s docs/docs.go "$TEMP_DIR/docs/docs.go" || \
  fail "docs/docs.go is out of date; run ./scripts/generate-docs.sh"
cmp -s docs/swagger.yaml "$TEMP_DIR/docs/swagger.yaml" || \
  fail "docs/swagger.yaml is out of date; run ./scripts/generate-docs.sh"
# swag omits the final newline from swagger.json, so normalize both files by
# reading and printing their lines before comparing their content.
cmp -s <(awk '1' docs/swagger.json) <(awk '1' "$TEMP_DIR/docs/swagger.json") || \
  fail "docs/swagger.json is out of date; run ./scripts/generate-docs.sh"

echo "✅ Pre-release checks passed."
