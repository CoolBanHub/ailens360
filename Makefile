SHELL := /bin/bash
BIN   := ailens360
PKG   := github.com/CoolBanHub/ailens360
VER   ?= 0.1.0-dev
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X $(PKG)/internal/version.Version=$(VER) \
           -X $(PKG)/internal/version.Commit=$(GIT_SHA) \
           -X $(PKG)/internal/version.BuildTime=$(DATE)

.PHONY: dev build run test lint tidy clean keygen docker docker-up docker-down

dev: run

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) ./cmd/ailens360

run:
	go run ./cmd/ailens360 server

keygen:
	go run ./cmd/ailens360 keygen

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
	  -t ailens360/ailens360:$(VER) \
	  -t ailens360/ailens360:latest \
	  -f deploy/docker/Dockerfile .

docker-up:
	docker compose -f docker-compose.deps.yml up -d

docker-down:
	docker compose -f docker-compose.deps.yml down
