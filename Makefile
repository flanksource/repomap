.PHONY: build install test test-unit fixtures lint fmt

build:
	go build -o .bin/repomap ./cmd/repomap

install: build
	cp .bin/repomap /usr/local/bin/

test: test-unit fixtures

test-unit:
	go test ./...

fixtures: build
	cd testdata && gavel fixtures 'scope-*.md'

lint:
	golangci-lint run

fmt:
	gofmt -s -w .
