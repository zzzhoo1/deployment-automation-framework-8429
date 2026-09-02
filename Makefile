.PHONY: build test run fmt vet docker docker-run

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

docker:
	docker build -t gdrive-bot .

docker-run:
	docker run --rm -it --env-file .env -v $(PWD)/data:/data -v $(PWD)/downloads:/downloads gdrive-bot
