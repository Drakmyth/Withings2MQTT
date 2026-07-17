# Contributing

Thank you for helping improve Withings2MQTT.

## Development workflow

1. Create a focused branch from the latest `master`.
2. Keep changes scoped to one feature or fix and add regression coverage beside the affected package.
3. Format changed Go files with `gofmt`.
4. Run `go test ./...`, `go build ./...`, and `go vet ./...`.
5. Open a pull request describing the behavior change, tests run, configuration or migration impact, and operational considerations.

Use concise conventional-style commit subjects such as `feat: add activity sync` or `fix: preserve cursor after failure`.

## Project constraints

The service intentionally uses Go's standard library, server-rendered templates, and progressively enhanced HTMX. Before proposing a new framework or dependency, explain why the existing approach cannot meet the requirement.

Never commit credentials, token-encryption keys, OAuth tokens, health databases, or real health data. Use synthetic values in tests and examples. Report suspected vulnerabilities according to [SECURITY.md](SECURITY.md), not through a public issue.
