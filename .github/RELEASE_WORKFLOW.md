# Release workflow

Releases are created from the `master` branch through the manual GitHub Actions
workflow `Build and Release`.

Before starting a release:

1. Run `go test ./...`, `go vet ./...` and `go build ./...` locally.
2. Confirm that the working tree is clean and the target version is new.
3. Dispatch the workflow with a semantic version such as `1.5.0`.

The workflow validates the version, regenerates the API documentation, builds
Linux, Windows and macOS binaries, creates SHA256 files and publishes the
GitHub release. Existing tags are never deleted or force-updated; a failed
release must be fixed and rerun with a new tag or after the failed run has been
removed manually.
