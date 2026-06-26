.PHONY: test race fuzz cover vet fmt lint build

test:
	go test ./...

race:
	go test -race ./...

fuzz:
	go test -run=XXX -fuzz=FuzzInto -fuzztime=30s ./coerce/

cover:
	go test -coverprofile=coverage.txt ./...
	go tool cover -html=coverage.txt -o coverage.html

vet:
	go vet ./...

fmt:
	gofmt -w .

lint:
	golangci-lint run

build:
	go build ./...
