# syntax=docker/dockerfile:1.6

FROM node:22-alpine AS ui-build
WORKDIR /ui

ENV PNPM_HOME=/pnpm
ENV PATH=${PNPM_HOME}:${PATH}

RUN corepack enable
RUN corepack prepare pnpm@8.15.9 --activate

COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/pnpm/store \
    pnpm install --frozen-lockfile

COPY frontend/ ./
RUN pnpm build

FROM golang:1.26.3-alpine AS build
WORKDIR /src

ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.org
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ARG NO_PROXY

ENV GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB} \
    HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    ALL_PROXY=${ALL_PROXY} \
    NO_PROXY=${NO_PROXY} \
    http_proxy=${HTTP_PROXY} \
    https_proxy=${HTTPS_PROXY} \
    all_proxy=${ALL_PROXY} \
    no_proxy=${NO_PROXY}

# Cache go mod downloads
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=0.1.0-dev
ARG COMMIT=docker
ARG BUILD_TIME=unknown

# Pure-Go build; storage is Postgres (pgx) + Redis (go-redis), both pure Go.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w \
      -X github.com/CoolBanHub/ailens360/internal/version.Version=${VERSION} \
      -X github.com/CoolBanHub/ailens360/internal/version.Commit=${COMMIT} \
      -X github.com/CoolBanHub/ailens360/internal/version.BuildTime=${BUILD_TIME}" \
    -o /out/ailens360 ./cmd/ailens360

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S ailens && adduser -S -G ailens ailens

WORKDIR /app
COPY --from=build /out/ailens360 /usr/local/bin/ailens360
COPY --from=ui-build /ui/dist /app/ui

RUN chown -R ailens:ailens /app

USER ailens
EXPOSE 8080 8081 8082

# Configuration is provided entirely via environment variables.
# Mount a .env file at /app/.env, or inject env vars directly via the runtime.
# Choose role via CMD: proxy / collector / api.
ENTRYPOINT ["/usr/local/bin/ailens360"]
CMD ["api"]
