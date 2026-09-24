.PHONY: all build run sim test clean docker-build docker-up docker-down docker-logs

all: build

build:
	mkdir -p bin
	go build -o bin/server ./cmd/server
	go build -o bin/simulator ./cmd/simulator

run:
	go run ./cmd/server

sim:
	go run ./cmd/simulator

test:
	go test -v -race ./...

clean:
	rm -rf bin/ fleet.db fleet.db-journal fleet.db-wal fleet.db-shm

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f
