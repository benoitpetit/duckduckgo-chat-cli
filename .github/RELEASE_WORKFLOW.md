# Release workflow

Releases are published by the manual GitHub Actions workflow `Build and
Release`. It uses the same verification and build scripts as local releases.

Before starting a release:

1. Run `./scripts/pre-release-check.sh` with the Go version required by `go.mod`.
2. Confirm that the working tree is clean and the target version is new.
3. Dispatch **Actions → Build and Release → Run workflow** with a semantic
   version such as `1.9.0`, or run `./scripts/release.sh 1.9.0` after pushing
   `master` (requires authenticated GitHub CLI).

The workflow validates the version, runs the full test and cross-build checks,
regenerates the API documentation, builds Linux, Windows and macOS binaries,
creates SHA256 files and publishes the GitHub release. `/speak` uses an installed
Chrome or Chromium browser and does not require a bundled native companion. The Windows `.exe`
embeds `docs/images/logo.png` as its application icon using the pinned `go-winres` tool;
Linux and macOS command-line binaries do not contain an application icon.
Existing tags are never deleted or force-updated; a failed release must be
fixed and rerun with a new version.
