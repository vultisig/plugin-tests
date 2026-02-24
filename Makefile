.PHONY: build-server build-worker build-testrunner sqlc fmt lint \
	docker-testrunner docker-server docker-worker docker-all

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
	docker build --platform linux/amd64 --build-arg SERVICE=testrunner -t plugin-tests-testrunner:dev .

docker-server:
	docker build --platform linux/amd64 --build-arg SERVICE=server -t plugin-tests-server:dev .

docker-worker:
	docker build --platform linux/amd64 --build-arg SERVICE=worker -t plugin-tests-worker:dev .

docker-all: docker-server docker-worker docker-testrunner
