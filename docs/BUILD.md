# Building and verifying releases

The project requires the Go version declared by `go.mod` (currently Go 1.24).
Chrome or Chromium 115+ is required at runtime for the Duck.ai browser
bootstrap; it is not required to compile the binaries.

## Local release build

From the repository root, run the build script:

```bash
./scripts/build.sh 1.2.3
```

Passing a version builds without an interactive prompt. Omitting it asks for a
version and confirmation. The script regenerates the REST API documentation,
builds Linux AMD64, Windows AMD64, macOS ARM64, and macOS AMD64 binaries, writes
SHA256 files for each binary, and creates a release ZIP under `build/`.
Checksums use `sha256sum` when available and fall back to `shasum -a 256` on
macOS.

The Windows executable embeds `logo.png` as its application icon. The builder
uses the pinned `go-winres` tool to create a temporary Windows resource object
and removes it after the `.exe` is linked. The Linux and macOS command-line
binaries do not contain an application icon; those formats require a separate
desktop or app bundle.

## Pre-release verification

Run the full local checks with:

```bash
./scripts/pre-release-check.sh
```

The checks validate the Go version from `go.mod`, module integrity, formatting,
`go vet`, all Go tests, Linux/macOS cross-builds, and a Windows build using the
embedded logo. The Windows resource generator is pinned in
`scripts/build_windows_binary.sh` and is downloaded by Go on first use.

The GitHub Actions workflow uses the same build and verification scripts. Start
it from **Actions → Build and Release → Run workflow** and provide a semantic
version such as `1.2.3`.
