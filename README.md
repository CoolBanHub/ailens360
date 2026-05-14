# AILens360

> **360° observability for every LLM call** —— 无需 SDK 的 AI 应用可观测平台

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](https://golang.org/)
[![Status](https://img.shields.io/badge/status-WIP-orange.svg)]()

---

## 这是什么

AILens360 是一个**开源、自部署、低代码侵入**的 AI 应用可观测性平台。

通常只需要把代码里的 `baseURL` 从 `https://api.openai.com/v1` 改成 `http://your-ailens360/p/https://api.openai.com/v1`（在代理前缀 `/p/` 后面**直接拼上完整的上游 URL**），再在请求头里加一个 `X-AILens-Project-Key: <控制台分配的项目密钥>`。原本的 `Authorization` / `x-api-key` 透传到上游，**AILens360 不持有、不存储真实上游 API Key**。

无需引入任何 SDK；多数 OpenAI 兼容 SDK 场景只需要调整 `baseURL`，应用代码几乎零改动。

## 解决什么问题

做过 AI 应用的开发者都遇到过：

1. **黑盒调用**：直接写 `https://api.openai.com/v1`，根本看不到真实的请求和响应到底是什么
2. **成本失控**：不知道每天烧了多少 token，也不知道哪个功能最贵
3. **Prompt 难调试**：模型输出不符合预期，但没有完整的上下文记录
4. **多模型混用**：OpenAI、Claude、国内大模型混着用，缺一个统一的观测视图

AILens360 用一个**反向代理 + 可视化控制台**的方式，让上面这些问题一次性解决。

## 核心特性

### 🔌 低侵入式集成
- 改 `baseURL` + 加一个请求头就能开始观测，不用引入 SDK
- 原 `Authorization` 直接透传到上游，AILens360 不持有真实 Key
- 上游地址直接拼到 baseURL 后面；已内建 OpenAI / Anthropic / Gemini 解析，并默认兼容多数 OpenAI 风格上游
- 项目密钥通过 `X-AILens-Project-Key` 请求头识别，与上游路径解耦
- SSE 解析器按上游 host 在内部选择（OpenAI / Anthropic / Gemini），调用方无感
- 流式（SSE）响应自动解析

### 📊 全链路可观测
- 记录每一次请求和响应，支持流式响应解析；原始 body 会受采集上限限制
- Token / Cost 统计（按模型、项目、用户、会话等维度聚合）
- 延迟指标（TTFT、TTFB、总耗时、TPS）
- 错误状态、终止状态与基础归因信息

### 🛠️ 当前控制台能力
- Web 控制台支持登录、项目管理、接入指引
- 项目概览页可查看聚合用量、成本和最近 trace
- Trace 列表 / 详情页支持按项目、用户、会话、model 等维度筛选
- Redis 实时指标接口已实现，前端页面以轮询方式刷新

### 🚀 简单到极致
- **依赖一键起**：`docker compose up -d` 起 Postgres + Redis，应用本机 `make run`
- **应用进程无状态**：Postgres 存数据 / Redis 做缓存与实时指标，扩副本只加进程
- **完全开源**：Apache 2.0 协议，可商用、可二开

### 🔒 数据自主可控
- 默认自部署，观测数据保存在你自己的 Postgres / Redis 中，不上报到第三方 SaaS
- 真实上游 API Key 永远不写入 AILens360 数据库；trace 落库前强制 REDACT `Authorization` / `Cookie` / `x-api-key` / `x-goog-api-key`
- 支持私有化部署到 K8s / Docker / 物理机
- 适合数据敏感场景

## 快速开始

> ⚠️ 项目处于早期开发阶段。核心代理、项目管理、trace 查询、用量统计与基础控制台已可运行；部分能力仍在持续补全。

### 核心思路：URL 内嵌上游 + 头部传项目密钥 + Authorization 透传

AILens360 的核心设计是 **"创建 Project → 拿 project_key → baseURL 直接拼上游 URL，请求头里塞 project_key"**。Project 是唯一的一级概念，**上游地址完全由客户端在 baseURL 里指定**，原 `Authorization` 直接透传到上游。

```
┌──────────────────────────────────┐
│ 控制台创建 Project:                │
│ project_key  = <64-char base62>   │
│ proxy_prefix = .../p              │
└──────────────┬───────────────────┘
               │ 应用 baseURL = proxy_prefix + 完整上游 URL
               │ 请求头 X-AILens-Project-Key: <project_key>
               │ 原 Authorization 透传到上游
               ▼
┌─────────────────────────────────────┐
│ AILens360 Proxy                      │
│ - parseProxyPath 拆出 内嵌上游 URL    │
│ - X-AILens-Project-Key 解析 Project   │
│ - 按上游 host 选 SSE 解析器           │
│ - 透传 Authorization 到上游           │
│ - 异步落库 + REDACT 敏感 Header       │
└──────────────┬──────────────────────┘
               ▼
┌─────────────────────────────────┐
│ 任意 HTTP 上游                   │
│ OpenAI / Anthropic / Gemini /    │
│ DeepSeek / Groq / 本地 vLLM ...  │
└─────────────────────────────────┘
```

### 三步上手

```bash
# 1. 启动依赖与应用（详见下文「部署」章节）
docker compose -f docker-compose.deps.yml up -d
make run

# 2. 先登录拿 JWT，再通过控制台 API 创建 Project
TOKEN=$(curl -sX POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jq -r '.data.token')

curl -X POST http://localhost:8080/api/projects \
     -H "Authorization: Bearer $TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"name":"my-app"}'
# → {
#     "data": {
#       "project_key": "kJ8s...64-char...",
#       "proxy_prefix": "http://localhost:8080/p",
#       "example": {
#         "openai":    "http://localhost:8080/p/https://api.openai.com/v1",
#         "anthropic": "http://localhost:8080/p/https://api.anthropic.com",
#         "gemini":    "http://localhost:8080/p/https://generativelanguage.googleapis.com/v1beta"
#       }
#     }
#   }

# 3. baseURL 拼完整上游 URL；project_key 进 X-AILens-Project-Key 头；
#    Authorization 用你真实的上游 Key
curl -X POST 'http://localhost:8080/p/https://api.openai.com/v1/chat/completions' \
     -H "Authorization: Bearer sk-real-openai-key" \
     -H "X-AILens-Project-Key: kJ8s...64-char..." \
     -H 'Content-Type: application/json' \
     -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
```

SDK 写法：

```python
# Before
client = OpenAI(api_key="sk-real-openai-key",
                base_url="https://api.openai.com/v1")

# After：base_url 在上游 URL 前拼 proxy_prefix，再通过 default_headers 注入项目密钥
client = OpenAI(
    api_key="sk-real-openai-key",
    base_url="http://localhost:8080/p/https://api.openai.com/v1",
    default_headers={"X-AILens-Project-Key": "kJ8s...64-char..."},
)
# SDK 会在 base_url 末尾自动追加 /chat/completions，
# 代理实际收到的请求是：
#   POST /p/https://api.openai.com/v1/chat/completions

# 接入 DeepSeek / Groq / 本地 vLLM 只需要换掉 base_url 里的上游 URL 段：
# base_url="http://localhost:8080/p/https://api.deepseek.com/v1"
# base_url="http://localhost:8080/p/http://localhost:11434/v1"  # Ollama
```

### 这样设计的好处

- **真实 API Key 不在 AILens360 落地**：透传到上游，trace 落库前强制 REDACT，AILens360 数据库被拷走也不泄露 Key
- **接入像 Langfuse 一样简单**：创建 Project → 拿 project_key → 改一行 baseURL + 加一个请求头，3 步搞定
- **任意上游都能进**：DeepSeek / Groq / Together / Moonshot / 本地 vLLM / Ollama / 自建 OpenAI 兼容服务，不需要在 AILens360 侧做任何 per-project 上游配置
- **按 Project 维度统计**：天然就有了项目 / 环境维度的成本与用量归因

### 当前未实现 / 规划中

- Prompt 版本管理与回放
- 多模型 A/B 对比
- OpenTelemetry / OTLP 导出与接收
- ClickHouse 明细存储
- Go 服务内直接托管前端静态资源

## 部署

> 尚未发布二进制 release，需要从源码构建。当前 `go.mod` 声明为 Go 1.26.1；运行依赖为 Postgres 与 Redis，应用本身纯 Go（无 CGO）。

### 方式一：二进制部署（推荐生产）

#### 1. 构建

```bash
git clone https://github.com/CoolBanHub/ailens360.git
cd ailens360

# 本机构建（输出到 ./bin/ailens360）
make build

# 或交叉编译 Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -ldflags "-s -w" -o bin/ailens360-linux-amd64 ./cmd/ailens360
```

#### 2. 准备配置与目录

```bash
sudo mkdir -p /opt/ailens360/{bin,data}
sudo cp bin/ailens360 /opt/ailens360/bin/
sudo cp .env.example /opt/ailens360/.env
sudo chmod 600 /opt/ailens360/.env
```

编辑 `/opt/ailens360/.env`，至少配置：

```bash
AILENS360_PROXY_ADDR=0.0.0.0:8080
AILENS360_DB_DSN=postgres://ailens:ailens@localhost:5432/ailens360?sslmode=disable
AILENS360_REDIS_ADDR=localhost:6379
AILENS360_JWT_SECRET=<openssl rand -hex 32>    # 必填，用于多副本共享会话
```

> 当前架构下，AILens360 不持有真实上游 Key，因此 **不需要配置 `crypto.master_key`**。
> `ailens360 keygen` 命令仍然保留，未来若需要加密某些字段会重新启用。

#### 3. 前台运行（验证用）

```bash
cd /opt/ailens360
./bin/ailens360 server   # 默认读取当前目录下的 .env
```

访问 `http://<host>:8080/healthz` 应返回 `ok`。

#### 4. 注册 systemd 服务（Linux 长期运行）

新建 `/etc/systemd/system/ailens360.service`：

```ini
[Unit]
Description=AILens360 — 360° observability for every LLM call
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ailens
Group=ailens
WorkingDirectory=/opt/ailens360
EnvironmentFile=/opt/ailens360/.env
ExecStart=/opt/ailens360/bin/ailens360 server
Restart=on-failure
RestartSec=3s
LimitNOFILE=65536

# 安全加固
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/opt/ailens360/data

[Install]
WantedBy=multi-user.target
```

启动：

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin ailens
sudo chown -R ailens:ailens /opt/ailens360/data
sudo systemctl daemon-reload
sudo systemctl enable --now ailens360
sudo systemctl status ailens360
sudo journalctl -u ailens360 -f
```

#### 5. 反向代理（可选，生产建议）

在前面挂一层 Nginx / Caddy 终止 TLS，并把 `/p/`（代理流量）和 `/api/`（管理 API）转给 `127.0.0.1:8080`。注意 **SSE 必须关闭 buffering、放宽超时**：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # SSE / 流式响应必需
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 1h;
    proxy_send_timeout 1h;
    chunked_transfer_encoding on;
}
```

#### 6. 升级与回滚

```bash
# 升级：替换二进制后重启即可，Postgres 迁移会在启动时自动执行
sudo systemctl stop ailens360
sudo cp bin/ailens360 /opt/ailens360/bin/ailens360
sudo systemctl start ailens360

# 回滚前请先备份数据库
pg_dump -h localhost -U ailens ailens360 > ailens360.sql.bak
```

### 方式二：Docker

> 单独 `docker run ailens360/ailens360` 不足以启动完整服务，因为 Postgres、Redis、JWT secret 都是必需项。更推荐直接使用下文的 `docker compose.yml`。

```bash
# 构建镜像（Makefile 已内置）
make docker

# 假设 Postgres / Redis 已存在，再单独启动应用容器
docker run -d --name ailens360 \
  -p 8080:8080 \
  -e AILENS360_DB_DSN='postgres://ailens:ailens@host.docker.internal:5432/ailens360?sslmode=disable' \
  -e AILENS360_REDIS_ADDR='host.docker.internal:6379' \
  -e AILENS360_AUTH_USERNAME='admin' \
  -e AILENS360_AUTH_PASSWORD='change-me' \
  -e AILENS360_JWT_SECRET="$(openssl rand -hex 32)" \
  --restart unless-stopped \
  ailens360/ailens360:latest
```

### 方式三：Docker Compose

仓库根目录提供两份 compose 文件，按需选用：

| 文件 | 内容 | 适用场景 |
|---|---|---|
| `docker-compose.deps.yml` | 仅 Postgres + Redis | 本地开发：依赖跑容器、应用用 `make run` 在宿主机跑 |
| `docker-compose.yml` | 应用（从源码构建） + Postgres + Redis | 自部署一台服务器，一条命令全跑起来 |

```bash
# 仅依赖（应用本机跑）
docker compose -f docker-compose.deps.yml up -d

# 全栈一键起（先写 .env 提供 JWT secret）
echo "AILENS360_JWT_SECRET=$(openssl rand -hex 32)" > .env
docker compose up -d --build
docker compose logs -f ailens360
```

或直接 `make docker-up` / `make docker-down`（默认走 `docker-compose.deps.yml`）。

### 前端控制台

Docker 镜像构建时会同时执行前端 `pnpm build`，并由 Go 服务直接托管静态资源；`docker compose up -d` 后直接访问 `http://localhost:8080/` 即可打开控制台。

本地开发可单独启动前端：

```bash
cd frontend
pnpm install
pnpm dev
```

默认前端开发服务器运行在 `http://localhost:5173`，通过 `/api` 调用后端 API。

### 配置覆盖优先级

`环境变量` > `配置文件` > `内置默认值`。常用环境变量：

| 变量 | 用途 |
|---|---|
| `AILENS360_AUTH_USERNAME` | 控制台登录用户名 |
| `AILENS360_AUTH_PASSWORD` | 控制台登录密码 |
| `AILENS360_JWT_SECRET` | JWT 签名密钥，多副本必须共享 |
| `AILENS360_DB_DSN` | 数据库 DSN，覆盖配置文件 |
| `AILENS360_REDIS_ADDR` | Redis 地址 |

## 架构概览

```
   你的应用                              AILens360                          LLM 服务商
┌──────────────┐ baseURL=/p/      ┌──────────────────────┐ Authorization 透传 ┌─────────────┐
│  Your App    │ {上游完整 URL}    │  Proxy Server         │ ──────────────────>│ OpenAI/     │
│  (任意语言)   │ + project_key 头 │  ┌────────────────┐   │                    │ Anthropic/  │
│              │ ──────────────>  │  │ parseProxyPath  │  │                    │ Gemini/     │
│              │ <──────────────  │  │ + project_key   │  │ <──────────────── │ DeepSeek/   │
└──────────────┘    流式响应       │  │   header 解析   │  │     流式响应       │ vLLM/...    │
                                   │  │ DetectFromHost  │  │                    └─────────────┘
                                   │  └───────┬────────┘   │
                                   └──────────┼───────────┘
                                             │ 异步落库（敏感 Header 已 REDACT）
                                             ▼
                                    ┌─────────────────┐
                                    │  Storage        │
                                    │  PostgreSQL     │
                                    │  + Redis (cache │
                                    │   / pubsub /    │
                                    │   realtime)     │
                                    └────────┬────────┘
                                             │
                                             ▼
                                    ┌─────────────────┐    ┌──────────────┐
                                    │  API Server     │ <─ │  Web Console │
                                    │  (Go)           │    │  (React)     │
                                    └─────────────────┘    └──────────────┘
```

详细设计见 [docs/architecture.md](./docs/architecture.md)。

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.26.1 + 标准库 + chi 路由 + pgx + slog |
| 前端 | React 18 / TypeScript / Vite / Tailwind |
| 存储 | PostgreSQL |
| 缓存 | hashicorp/golang-lru (L1 进程) + Redis (L2 + Pub/Sub 失效广播) |
| 实时指标 | Redis 计数器（滑动窗口） |
| 部署 | 单二进制 / Docker / Docker Compose |

## 文档

- [快速开始](./docs/getting-started.md)
- [部署指南](./docs/deployment.md)
- [系统架构设计](./docs/architecture.md)
- [API 与代理协议设计](./docs/api-design.md)
- [项目目录结构](./docs/project-structure.md)
- [贡献指南](./docs/contributing.md)

## 贡献

欢迎 Issue、PR、Discussion。项目处于早期阶段，每一个意见都很重要。

## License

[Apache License 2.0](./LICENSE)
