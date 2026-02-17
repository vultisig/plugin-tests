.PHONY: build-api sqlc fmt lint

build-api:
	go build -o bin/api ./cmd/api

sqlc:
	sqlc generate

fmt:
	gofmt -w .

lint:
	golangci-lint run ./...
