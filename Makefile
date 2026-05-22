SHELL := /bin/bash
BIN   := ailens360
PKG   := github.com/CoolBanHub/ailens360
VER   ?= 0.1.0-dev
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X $(PKG)/internal/version.Version=$(VER) \
           -X $(PKG)/internal/version.Commit=$(GIT_SHA) \
           -X $(PKG)/internal/version.BuildTime=$(DATE)

.PHONY: dev build run run-proxy run-collector run-api test lint tidy clean docker docker-up docker-down docker-build-up docker-build-down docker-deps-up docker-deps-down

dev: run

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) ./cmd/ailens360

# `make run` brings up all three processes in the background using a single
# subshell, so SIGINT/SIGTERM tears them all down together. Logs are interleaved
# to the terminal — for richer multi-pane logs use tmux or a process manager.
run:
	@trap 'kill 0' INT TERM EXIT; \
	  ( go run ./cmd/ailens360 collector & \
	    go run ./cmd/ailens360 api & \
	    go run ./cmd/ailens360 proxy & \
	    wait )

run-proxy:
	go run ./cmd/ailens360 proxy

run-collector:
	go run ./cmd/ailens360 collector

run-api:
	go run ./cmd/ailens360 api

test:
	go test ./...

lint:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin dist data

docker:
	docker build \
	  --build-arg VERSION=$(VER) \
	  --build-arg COMMIT=$(GIT_SHA) \
	  --build-arg BUILD_TIME=$(DATE) \
	  -t coolbanhub/ailens360:$(VER) \
	  -t coolbanhub/ailens360:latest \
	  -f deploy/docker/Dockerfile .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-build-up:
	docker compose up -d --build

docker-build-down:
	docker compose -f docker-compose.yml -f docker-compose.build.yml down

docker-deps-up:
	docker compose -f docker-compose.deps.yml up -d

docker-deps-down:
	docker compose -f docker-compose.deps.yml down
