# Build the static service binary.
FROM golang:1.23-bookworm AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ackermann-server ./cmd/server

# Minimal runtime image based on Debian slim (Docker Hub, widely mirrorable).
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system app && useradd --system --gid app --home /nonexistent app
COPY --from=build /out/ackermann-server /usr/local/bin/ackermann-server
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ackermann-server"]
