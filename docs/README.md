# API Documentation

This directory contains the automatically generated API documentation for the Duck.ai Chat CLI.

## Files

- **`docs.go`** - Generated Go code containing the Swagger specification
- **`swagger.json`** - JSON format of the API specification
- **`swagger.yaml`** - YAML format of the API specification (human-readable)
- **`COMMAND_CONSISTENCY.md`** - Documentation about command consistency system

## Automatic Generation

The API documentation is automatically generated from annotations in `internal/api/docs.go` using [swaggo/swag](https://github.com/swaggo/swag).

### Generation Process

The documentation is automatically generated during:

1. **Local builds** - Run `./scripts/build.sh`
2. **CI/CD pipeline** - Both test and release workflows
3. **Manual generation** - Run `./scripts/generate-docs.sh`

### Source of Truth

The source of truth for API documentation is **`internal/api/docs.go`** which contains:

- API metadata (title, version, contact, etc.)
- Endpoint definitions via Go annotations
- Response models and examples

## Accessing Documentation

When the API server is running, the interactive Swagger UI is available at:

```
http://127.0.0.1:8080/doc/index.html
```

## Manual Generation

To manually regenerate the documentation:

```bash
# Using the dedicated script (recommended)
./scripts/generate-docs.sh

# Or directly with swag
swag init --generalInfo internal/api/docs.go --output docs/ --parseInternal
```

## Important Notes

1. **Do not edit the generated files directly** - They will be overwritten
2. **Always update `internal/api/docs.go`** for any documentation changes
3. **The files are version controlled** to ensure consistency across environments
4. **Pre-release checks validate** that documentation is up-to-date

## Validation

The consistency of the documentation is validated by:

- **Pre-release checks** (`./scripts/pre-release-check.sh`)
- **CI/CD workflows** (test and release)
- **Build scripts** (automatic regeneration)

## Making Changes

To update the API documentation:

1. Edit the annotations in `internal/api/docs.go`
2. Run `./scripts/generate-docs.sh` to regenerate files
3. Commit all changes (source + generated files)
4. The CI/CD pipeline will validate consistency

## Current Version

- **API Version**: 1.4.0
- **Contact**: devbyben (contact@devbyben.fr)
- **Repository**: https://github.com/benoitpetit/duckduckgo-chat-cli
- **Host**: 127.0.0.1:8080 (default development)
