.PHONY: build-server build-worker build-testrunner sqlc fmt lint docker-testrunner

build-server:
	go build -o bin/server ./cmd/server

build-worker:
	go build -o bin/worker ./cmd/worker

build-testrunner:
	CGO_ENABLED=1 go build -o bin/testrunner ./cmd/testrunner

sqlc:
	sqlc generate

fmt:
	gofmt -w .

lint:
	golangci-lint run ./...

docker-testrunner:
	docker build --build-arg SERVICE=testrunner -t plugin-tests-testrunner:dev .
