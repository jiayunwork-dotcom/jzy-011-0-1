.PHONY: build test test-race run compose-up compose-down fmt vet

build:
	go build -o bin/ackermann ./cmd/ackermann

run:
	go run ./cmd/ackermann

test:
	go test ./...

test-race:
	go test -race ./...

# Requires: docker compose up -d db
test-postgres:
	ACKERMANN_TEST_DSN="postgres://ackermann:ackermann@localhost:5432/ackermann?sslmode=disable" \
		go test ./storage/ -run TestPostgresRoundTrip -v

fmt:
	gofmt -s -w .

vet:
	go vet ./...

compose-up:
	docker compose up -d --build

compose-down:
	docker compose down
