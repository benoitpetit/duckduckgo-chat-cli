# Building and verifying releases

The project requires the Go version declared by `go.mod` (currently Go 1.26).
Chrome or Chromium 115+ is required at runtime for Duck.ai browser access and
`/speak`; neither browser is required to compile the CLI. Linux builds do not
need GTK, WebKitGTK, or GStreamer development packages.

The resident tray uses a pure-Go tray backend and does not add Linux GTK build
dependencies. Linux global shortcuts use the desktop portal on Wayland or X11
key grabs on X11. Windows and macOS use their native shortcut registration;
macOS can require Accessibility/Input Monitoring permission at runtime.

## Run the CLI from source

From the repository root, start the CLI with the project launcher:

```bash
./scripts/run.sh
```

The launcher stores Go's cache and temporary files under `build/`, which avoids
common `/tmp` quota issues. To invoke Go directly, use the package path so it
includes every CLI source file:

```bash
mkdir -p build/go-cache build/go-tmp
GOCACHE="$PWD/build/go-cache" \
  TMPDIR="$PWD/build/go-tmp" GOTMPDIR="$PWD/build/go-tmp" \
  go run ./cmd/duckchat
```

If Chrome or Chromium is missing, install either browser with your operating
system's package manager and ensure its executable is available to the CLI.
The interactive chat and `/speak` both use the browser for Duck.ai access;
`/speak` opens a separate app-style browser window and requests microphone
permission when the user starts a call. After the terms are accepted, a normal
interactive launch also starts or reuses the background tray service. The
service stays running after the terminal closes and can be stopped from its
tray menu.

## Local release build

From the repository root, run the build script:

```bash
./scripts/build.sh 1.9.0
```

Passing a version builds without an interactive prompt. Omitting it asks for a
version and confirmation. The script regenerates the REST API documentation,
builds Linux AMD64 and Windows AMD64 binaries, writes SHA256 files, and creates
a partial release ZIP under `build/`. macOS binaries are built on native macOS
runners by the GitHub release workflow, which assembles the complete
cross-platform ZIP. Checksums use `sha256sum` when available and fall back to
`shasum -a 256` on macOS.

The Windows executable embeds `docs/images/logo.png` as its application icon.
The builder uses the pinned `go-winres` tool to create a temporary Windows
resource object and removes it after the `.exe` is linked.

## Pre-release verification

Run the full local checks with:

```bash
./scripts/pre-release-check.sh
```

The checks validate the Go version from `go.mod`, module integrity, formatting,
`go vet`, all Go tests, the Linux build, and a Windows build using the embedded
logo. The Windows resource generator is pinned in
`scripts/build_windows_binary.sh` and is downloaded by Go on first use.

The GitHub Actions workflow uses the same build and verification scripts. Start
it from **Actions → Build and Release → Run workflow** and provide a semantic
version such as `1.9.0`. After committing and pushing `master`, the same workflow
can be dispatched from a terminal with `./scripts/release.sh 1.9.0`; this
requires an authenticated GitHub CLI (`gh auth login`).
