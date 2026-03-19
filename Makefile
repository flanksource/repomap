.PHONY: build install test lint fmt

build:
	go build -o .bin/repomap ./cmd/repomap

install: build
	cp .bin/repomap /usr/local/bin/

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -s -w .
