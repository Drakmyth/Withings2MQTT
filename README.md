# Withings2MQTT

Withings2MQTT is a local service that will link Withings accounts, persist their health data, and publish it to MQTT. This first release establishes its secure local server and storage foundation.

## Run locally

Generate an encryption key once and keep it secret; it encrypts OAuth tokens before they reach SQLite.

```sh
export WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY="$(openssl rand -base64 32)"
go run .
```

The service listens on `127.0.0.1:8080` by default. Visit `http://127.0.0.1:8080/healthz` for a health check and `http://127.0.0.1:8080/admin` for the local administration page. Data is stored at `data/withings2mqtt.db` by default.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY` | required | Base64-encoded, 32-byte AES-GCM key. |
| `WITHINGS2MQTT_LISTEN_ADDR` | `127.0.0.1:8080` | HTTP listener address. |
| `WITHINGS2MQTT_DB_PATH` | `data/withings2mqtt.db` | SQLite database path. |
| `WITHINGS2MQTT_ADMIN_ALLOW_REMOTE` | `false` | Allow `/admin` from non-loopback clients. |

The admin handler additionally rejects non-loopback requests unless `WITHINGS2MQTT_ADMIN_ALLOW_REMOTE=true`. Do not expose it publicly without appropriate network controls.

## Docker

```sh
docker build -t withings2mqtt .
docker run --rm -p 8080:8080 -v withings2mqtt-data:/data \
  -e WITHINGS2MQTT_TOKEN_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
  withings2mqtt
```

The image includes a Docker health check. It listens on all interfaces inside the container, but its admin UI remains localhost-only unless explicitly enabled.

## Development

```sh
go test ./...
go build ./...
```

HTMX 2.0.4 is vendored at `internal/app/static/htmx-2.0.4.min.js`; the admin UI makes no external browser asset request.
