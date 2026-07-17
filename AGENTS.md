# Repository Guidelines

## Project Structure & Module Organization

This is a Go service module (`github.com/Drakmyth/withings2mqtt`). `main.go` wires configuration, the HTTP server, signal handling, and graceful shutdown. Keep application code under `internal/`:

- `internal/app/`: HTTP routes, templates, embedded static assets, and application orchestration.
- `internal/config/`: typed environment-variable configuration.
- `internal/crypto/`: OAuth-token encryption helpers.
- `internal/store/`: SQLite access and embedded SQL migrations in `migrations/`.
- `internal/withings/`: Withings OAuth and health-data API clients.

Place package tests beside their code as `*_test.go`. The local HTMX asset and HTML templates belong under `internal/app/static/` and `internal/app/templates/`. Update `README.md` and `plan.md` when public configuration or planned scope changes.

Keep the frontend server-rendered, responsive, and usable without JavaScript. HTMX may progressively enhance interactions, but every workflow must retain a functional standard HTTP fallback.

## Build, Test, and Development Commands

Use Go 1.26.5.

```sh
go test ./...       # run all package tests
go build ./...      # compile every package
go vet ./...        # run static analysis
go run .            # start the service locally
docker build -t withings2mqtt .
docker compose up -d --build
```

Set `WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY` to a base64-encoded 32-byte key before starting the service. `WITHINGS2MQTT_DB_PATH` defaults to `data/withings2mqtt.db`.

## Coding Style & Naming Conventions

Use standard Go formatting: run `gofmt -w` on changed `.go` files before committing. Prefer small, focused packages and explicit error wrapping (`fmt.Errorf("operation: %w", err)`). Use exported PascalCase names only for cross-package APIs; keep helpers camelCase. Name migrations in ascending order, for example `003_<description>.sql`.

## Testing Guidelines

Use Go's built-in `testing` package and table-driven tests when multiple cases share behavior. Name tests `Test<Behavior>`, such as `TestAdminRejectsRemote`. Cover validation failures, persistence/restart behavior, and HTTP status or response behavior for each feature. Run `go test ./...`, `go build ./...`, and `go vet ./...` before pushing; add regression tests for fixes. When changing Go dependencies, run `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...`.

## Commit & Pull Request Guidelines

Use concise conventional-style subjects, as in `feat: establish HTTP server foundation` and `fix: address server foundation review`. Keep commits atomic. Create one short-lived feature branch per vertical milestone. PRs must describe included work, tests run, configuration or migration impact, and any operational considerations. Respond to review comments with the relevant commit and verification result. Do not merge a PR until the user has reviewed and explicitly approved it.

## Security & Configuration

Never commit OAuth credentials, database files, or token-encryption keys. OAuth tokens must be encrypted before SQLite persistence. Keep `/admin` localhost-only by default; any public exposure requires deliberate network and authentication controls.

Docker Compose binds the published HTTP port to `127.0.0.1`. It sets `WITHINGS2MQTT_ADMIN_ALLOW_REMOTE=true` only because requests crossing Docker's bridge do not appear to originate from loopback inside the container. Do not broaden the host binding while remote admin access is enabled unless authentication and appropriate network controls are added.
