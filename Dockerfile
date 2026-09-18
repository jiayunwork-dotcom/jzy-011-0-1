# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.23-bookworm AS build
WORKDIR /src

# Cache module downloads independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w" -o /out/ackermann ./cmd/ackermann

# ---- runtime stage ----
FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /
COPY --from=build /out/ackermann /ackermann
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/ackermann"]
