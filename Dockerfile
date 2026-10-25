# tDocs — single binary, pure Go (modernc.org/sqlite, no CGO).
# Build:  docker build -t tdocs .
# Run:    docker compose up -d   (baca .env via env_file)

# Keep the runtime layer dependency-free: no `apk add` at build time, so the
# build cannot break on flaky registry mirrors (the golang stage already ran
# `go mod download` over the network anyway). The binary only needs the
# system CA bundle for TLS to Telegram/my.telegram.org, copied from the
# build stage. Timezone data ships inside the Go runtime; server timestamps
# render in UTC in this image, matching the SQLite UTC default everywhere.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tdocs ./cmd/tdocs

# Small runtime on plain alpine (no package fetch): only the CA bundle plus
# the unprivileged user.
FROM alpine:3.21
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN adduser -D -h /data tdocs
COPY --from=build /out/tdocs /usr/local/bin/tdocs
USER tdocs
WORKDIR /data
VOLUME ["/data"]
ENV TDOCS_DB_PATH=/data/tdocs.db \
    TDOCS_HOST=0.0.0.0 \
    TDOCS_PORT=8080 \
    TDOCS_NO_BROWSER=1
EXPOSE 8080
ENTRYPOINT ["tdocs"]
CMD ["server"]
