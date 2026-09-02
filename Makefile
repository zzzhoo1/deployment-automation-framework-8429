.PHONY: build test run fmt vet

build:
	go build ./...

test:
	go test ./...

run:
	go run ./cmd/gdrive-bot

fmt:
	gofmt -w .

vet:
	go vet ./...
