
.PHONY: db run test

db:
	podman compose up -d db

run:
	go run ./cmd/server

test:
	go test ./...
