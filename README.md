# Withings2MQTT

Withings2MQTT is a local service that links Withings accounts, persists their health data, and will publish it to MQTT. The project is under active development; account linking and scale-data synchronization are implemented, while activity, sleep, background sync, and MQTT publishing remain planned work.

Health measurements and OAuth credentials are sensitive. Keep the service local, protect its database and encryption key, and review [SECURITY.md](SECURITY.md) before deploying it beyond a trusted machine.

## Run locally

Generate an encryption key once and keep it secret; it encrypts OAuth tokens before they reach SQLite.

```sh
export WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY="$(openssl rand -base64 32)"
go run .
```

Alternatively, copy `.env.example` to `.env`, generate the required encryption key, and fill in Withings OAuth settings when you are ready to link an account. `.env` is ignored by Git and loaded by the included VS Code launch configuration and Docker Compose.

The service listens on `127.0.0.1:8080` by default. Visit `http://127.0.0.1:8080/healthz` for a health check and `http://127.0.0.1:8080/admin` for the local administration page. Data is stored at `data/withings2mqtt.db` by default.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY` | required | Base64-encoded, 32-byte AES-GCM key. |
| `WITHINGS2MQTT_LISTEN_ADDR` | `127.0.0.1:8080` | HTTP listener address. |
| `WITHINGS2MQTT_DB_PATH` | `data/withings2mqtt.db` | SQLite database path. |
| `WITHINGS2MQTT_ADMIN_ALLOW_REMOTE` | `false` | Allow `/admin` from non-loopback clients. |
| `WITHINGS2MQTT_WITHINGS_CLIENT_ID` | empty | Withings developer application client ID. |
| `WITHINGS2MQTT_WITHINGS_CLIENT_SECRET` | empty | Withings developer application client secret. |
| `WITHINGS2MQTT_WITHINGS_REDIRECT_URL` | empty | OAuth callback URL registered with Withings. |

The admin handler additionally rejects non-loopback requests unless `WITHINGS2MQTT_ADMIN_ALLOW_REMOTE=true`. Do not expose it publicly without appropriate network controls.
The three Withings settings may be omitted while the service is unlinked, but all three are required before starting account linking.

## Scale data

After linking an account, use **Sync new data** beside that account on the admin page. The service follows all Withings result pages, imports new and corrected scale groups since the last successful sync, deduplicates repeated results, and stores normalized weight and available body-composition measurements in SQLite. A failed fetch or import does not advance the account's weight cursor. Use **Resync all scale data** to repair an incomplete historical import without deleting existing samples.

Supported normalized metrics are weight, fat-free mass, fat ratio, fat mass, muscle mass, hydration, and bone mass. Body-composition fields are optional; weight-only measurements import normally.

## Docker

The simplest container setup uses Docker Compose. Generate an encryption key, then start the service:

```sh
export WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY="$(openssl rand -base64 32)"
docker compose up -d --build
```

Visit `http://localhost:8080/admin`. Stop the service with `docker compose down`; the named volume preserves the database.

If port 8080 is already in use, select another host port before starting Compose. For example, in PowerShell:

```powershell
$env:WITHINGS2MQTT_HTTP_PORT = "8081"
docker compose up -d --build
```

Then visit `http://localhost:8081/admin`. This setting changes only the host-side Compose port; the service continues to listen on port 8080 inside the container. If Withings OAuth is configured, its registered redirect URL must use the selected host port.

The Compose port is bound to `127.0.0.1` on the host. It enables remote admin requests inside the container because Docker bridge traffic does not appear as loopback to the service, while the host binding keeps the UI local-only.

To run the image without Compose:

```sh
docker build -t withings2mqtt .
docker run --rm -p 127.0.0.1:8080:8080 -v withings2mqtt-data:/data \
  -e WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
  -e WITHINGS2MQTT_ADMIN_ALLOW_REMOTE=true \
  withings2mqtt
```

The image includes a Docker health check. If you publish the container on a non-loopback host interface, do not enable remote admin access without adding authentication and appropriate network controls.

## Development

```sh
go test ./...
go build ./...
go vet ./...
```

HTMX 2.0.4 is vendored at `internal/app/static/htmx-2.0.4.min.js`; the admin UI makes no external browser asset request.

Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow and project constraints.

## License

Withings2MQTT is available under the [MIT License](LICENSE.md). The vendored HTMX asset is distributed under the Zero-Clause BSD license included beside the asset.
