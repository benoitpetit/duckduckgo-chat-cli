# Build and release

The project uses the Go version declared in `go.mod` (currently Go 1.27). Chrome or Chromium is needed at runtime for Duck.ai chat and voice, but not to compile the CLI. Linux builds do not require GTK, WebKitGTK, or GStreamer development packages.

## Get the source

```sh
git clone https://github.com/benoitpetit/duckduckgo-chat-cli.git
cd duckduckgo-chat-cli
```

The project replaces `github.com/c-bata/go-prompt` with the local copy in `third_party/go-prompt`. Keep that directory when building from a checkout.

## Run from source

Use the project launcher:

```sh
./scripts/run.sh
```

Or run the CLI package directly:

```sh
go run ./cmd/duckchat
```

The launcher stores the Go build cache and temporary files under `build/`. This can help on systems where `/tmp` has a small quota.

## Build locally

Build the Linux and Windows release binaries with a semantic version, for example:

```sh
./scripts/build.sh 1.9.2
```

The script writes Linux AMD64 and ARM64 binaries, Windows AMD64 and ARM64 executables, their checksums, and a release archive under `build/`. macOS binaries are built on native macOS GitHub Actions runners and added to the complete release archive. Windows ARM64 and AMD64 executables each embed the application icon.

The Windows executable embeds `docs/images/logo.png` as its application icon. The build script uses a pinned `go-winres` version to create the Windows resources.

## Run checks

Before opening a pull request, run:

```sh
go test ./...
go test -race ./...
go vet ./...
./scripts/pre-release-check.sh
```

The pre-release script checks the Go version, module integrity, formatting, tests, and Linux and Windows builds. It also confirms that generated API docs match their annotations. The GitHub test workflow adds import, static analysis, security, and cross-compilation checks.

Generate the API reference after changing annotations in `internal/api/docs.go`:

```sh
./scripts/generate-docs.sh
```

Commit the generated files under `docs/` with the source changes.

## Publish a release

Push the release commit to `master`, then start the **Build and Release** workflow from GitHub Actions and enter the version to publish in `X.Y.Z` format. The workflow builds and verifies the platform binaries, creates the matching tag, and publishes the GitHub release.

You can dispatch the same workflow from a terminal after authenticating `gh`:

```sh
gh auth login
./scripts/release.sh 1.9.2
```

Replace `1.9.2` with the version to publish. The release script checks that the working tree is clean, the current commit matches `origin/master`, and the version tag is unused before dispatching the workflow.
