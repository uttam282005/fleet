.PHONY: all build run sim test clean

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
