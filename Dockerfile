# Production Build Stage
FROM golang:1.26.5-alpine AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . ./

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /withings2mqtt .

# Production Release Stage. Alpine provides wget for the Docker health check.
FROM alpine:3.24
WORKDIR /app
RUN adduser -D -u 1001 appuser && mkdir -p /data && chown appuser:appuser /data
COPY --from=build /withings2mqtt /usr/local/bin/withings2mqtt
ENV WITHINGS2MQTT_LISTEN_ADDR=0.0.0.0:8080
ENV WITHINGS2MQTT_DB_PATH=/data/withings2mqtt.db
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O - http://127.0.0.1:8080/healthz || exit 1
USER appuser
ENTRYPOINT ["/usr/local/bin/withings2mqtt"]
