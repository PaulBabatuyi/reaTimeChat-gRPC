BINARY = reaTimeChat-api
APP_DIR = ./cmd/api

.PHONY: all build test unit integration docker-build docker-up docker-test fmt vet clean

all: build

build:
	go build -v -o bin/$(BINARY) $(APP_DIR)

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

unit:
	go test ./... -run Test -v

integration:
	# requires MONGODB_URI (docker-compose file provides one pointing to mongo)
	if [ -z "${MONGODB_URI}" ]; then echo "MONGODB_URI not set"; exit 1; fi
	go test ./... -v

docker-build:
	docker build -t paulbabatuyi/rea-time-chat-api:latest .

docker-check:
	@docker info >/dev/null 2>&1 || (echo "Docker daemon not reachable — start Docker Desktop / Docker Engine" && exit 1)

docker-up:
	docker compose up -d

docker-test:
	docker compose run --rm test-runner

clean:
	rm -rf bin/*
