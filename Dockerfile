# syntax=docker/dockerfile:1

# Build a static binary, then copy only that into a minimal, non-root image.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/keyrotator ./cmd/keyrotator
# Create the data directory here, owned by the nonroot user (65532), because
# the distroless image has no shell to do it. A fresh named volume copies this
# ownership, so the config can be written.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/keyrotator /usr/local/bin/keyrotator
COPY --from=build --chown=65532:65532 /out/data /data

# Config and its backups live on a volume so they survive image upgrades.
# Listen on all interfaces inside the container; publish the port on the host
# only (see docker-compose.yml) unless you have set a proxy token.
VOLUME ["/data"]
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/keyrotator", "-config", "/data/config.json", "-listen", "0.0.0.0:8787", "-quiet"]
