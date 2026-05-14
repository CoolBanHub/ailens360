# 项目目录结构

> 后端从零搭建，不使用 Kratos / Gin 等大型框架，只依赖 Go 标准库 + 少量轻量包。

## 零、已确认的技术决策

> 这里集中记录所有关键技术选型，作为后续开发的"宪法"。任何变更必须在此文档同步。

| # | 决策项 | 选择 | 理由摘要 |
|---|---|---|---|
| **D1** | Web 框架 | **不使用框架**，纯 stdlib + chi | 项目核心是反向代理 + 异步采集，框架带来的抽象多余；100% 自主控制 |
| **D2** | HTTP 端口策略 | 默认单端口 `:8080`，对外统一入口；大规模部署时可在反代层拆分 `/p/`、`/api/` 到不同进程 | 开发和单机部署最简单；扩缩容时再拆 |
| **D3** | 路由库 | `go-chi/chi/v5` | 轻量、stdlib 兼容、中间件机制清晰、社区主流 |
| **D4** | DB 访问层 | **手写 SQL + `database/sql`**（`jackc/pgx/v5` 驱动），不引入 sqlc / ORM | 表结构稳定、查询不复杂；ORM 的好处覆盖不了它的隐式行为 |
| **D5** | 消息队列（v0.4+） | NATS JetStream 默认，Kafka 可选 | 单二进制、运维简单；通过事件接口抽象，可切 Kafka |
| **D6** | 依赖注入 | **手写构造函数**，不用 wire/fx | 项目规模不需要；显式即文档；IDE 跳转友好；测试最易写 |
| **D7** | 仓储模式 | 接口与实现分离 | 业务代码只依赖 `internal/storage/repo/` 接口，可平滑切换 Postgres → ClickHouse |
| **D8** | 缓存抽象 | L1 进程 LRU + L2 Redis + Pub/Sub 失效广播 | 多副本一致；业务读写无感 |
| **D9** | 日志 | `log/slog`（Go 1.21+ 标准库） | 标准化、结构化、零依赖 |
| **D10** | 配置 | 环境变量（`.env` 由 `godotenv` 加载） | 12-factor；不引入 yaml |

## 一、整体目录

```
ailens360/
├── cmd/
│   └── ailens360/
│       └── main.go                    # 唯一入口，组装所有依赖并启动
│
├── internal/                          # 内部代码，不对外暴露
│   ├── proxy/                         # 【代理层】反向代理 + 指标采集
│   │   ├── handler.go                 # 代理入口：parseProxyPath + REDACT + pass-through
│   │   ├── sink.go                    # trace 异步落库 sink
│   │   ├── intercept/                 # ResponseWriter 拦截器（同时写客户端 + buffer）
│   │   └── stream/                    # SSE / chunked 流处理
│   │       ├── parser.go              # 通用 SSE 帧解析；NewParserForHost(host) 按上游 host 选解析器
│   │       ├── openai_parser.go       # OpenAI 事件归一化
│   │       ├── anthropic_parser.go    # Anthropic 事件归一化
│   │       ├── gemini_parser.go       # Gemini 事件归一化
│   │       └── types.go               # StreamEvent / Timeline / 派生指标
│   │   # 所有上游共用一个 pass-through 转发器；SSE 解析器按上游 host 在 stream 包内部分流。
│   │
│   ├── api/                           # 【控制台 API】给 Web Console 用
│   │   ├── router.go                  # chi 路由组装
│   │   ├── middleware/                # CORS / JWT / 日志 / recover
│   │   ├── handler/
│   │   │   ├── handlers.go            # projects / traces / trace_groups / metrics/usage
│   │   │   └── auth.go                # /auth/login + /auth/me
│   │   └── response/                  # 统一响应格式
│   │
│   ├── auth/                          # 控制台 JWT 服务
│   ├── collector/                     # channel pipeline + 批量落库
│   ├── project/                       # Project CRUD + project_key resolver
│   ├── cache/                         # L1 LRU + L2 Redis + Pub/Sub 失效广播
│   ├── metrics/                       # Redis 实时计数器
│   ├── tokenizer/                     # tiktoken（OpenAI）+ Anthropic / Gemini 估算
│   ├── pricing/                       # 模型价格表 + 动态刷新（refresher）
│   ├── crypto/                        # AES-256-GCM keygen（dormant，运行时未启用）
│   ├── storage/
│   │   ├── repo/                      # 仓储接口定义（ProjectRepo / TraceRepo + 数据结构）
│   │   └── postgres/                  # Postgres 实现
│   │       ├── db.go                  # 连接 + 自动 apply migrations
│   │       ├── project_repo.go
│   │       ├── trace_repo.go
│   │       └── migrations/            # 0001_init.up.sql（合并版基线）
│   │
│   ├── config/                        # 配置结构体 + .env 加载
│   ├── logger/                        # slog 配置
│   ├── version/                       # 编译期注入版本号
│   └── app/
│       └── app.go                     # App 组装：构造所有组件 + 启动 / 优雅关闭
│
├── pkg/                               # 对外可复用的工具包
│   ├── shortid/                       # 短 ID 生成（base62）
│   └── sse/                           # SSE 通用解析工具
│
├── frontend/                            # React 前端（开发时独立 Vite；Docker 镜像会一并构建并由 Go 托管）
│   ├── src/
│   │   ├── pages/
│   │   ├── components/
│   │   ├── api/                       # 后端 API client
│   │   └── store/
│   ├── package.json
│   └── vite.config.ts
│
├── deploy/
│   └── docker/Dockerfile
│
├── docker-compose.yml                 # 全栈：应用 + Postgres + Redis
├── docker-compose.deps.yml            # 仅依赖：Postgres + Redis（本机 `make run` 用）
│
├── docs/                              # 设计文档
│   ├── README.md
│   ├── getting-started.md
│   ├── deployment.md
│   ├── architecture.md
│   ├── api-design.md
│   ├── project-structure.md
│   └── contributing.md
│
├── .env.example
├── go.mod / go.sum
├── Makefile
├── README.md
└── LICENSE
```

## 二、关键设计原则

### 2.1 默认单端口，可在反代层拆分

本地开发和单机部署默认只暴露 `:8080`，同时提供 `/p/...` 代理入口（项目身份通过 `X-AILens-Project-Key` 请求头识别）、`/api/...` 控制台 API、`/healthz` 与 `/version`。

代理流量（高并发、长 SSE）和控制台 API（CRUD、低频）特征完全不同，大规模部署时可以在反向代理 / K8s Ingress 层拆分：
- proxy-deployment 只跑代理逻辑
- api-deployment 跑控制台 + 静态前端

两者共享 Postgres / Redis。无需改业务代码。

### 2.2 internal vs pkg

- `internal/`：项目私有，不允许其他项目 import
- `pkg/`：通用工具（短 ID、SSE 解析等），可被未来的 SDK / CLI 复用

### 2.3 仓储模式（Repository Pattern）

`storage/repo/types.go` 只定义**接口** + 数据结构（`Project` / `Trace` / `TraceGroup`），具体实现放在 `storage/postgres/`。业务代码只依赖接口，**永远不直接 import 具体实现**。

```go
// internal/storage/repo/types.go
type TraceRepo interface {
    Create(ctx context.Context, t *Trace) error
    GetByID(ctx context.Context, id string) (*Trace, error)
    List(ctx context.Context, filter ListFilter) ([]*Trace, int, error)
    BatchCreate(ctx context.Context, ts []*Trace) error
}

type ProjectRepo interface {
    Create(ctx context.Context, p *Project) error
    GetByID(ctx context.Context, id string) (*Project, error)
    GetByProjectKey(ctx context.Context, key string) (*Project, error)
    List(ctx context.Context) ([]*Project, error)
    Update(ctx context.Context, p *Project) error
    Delete(ctx context.Context, id string) error
}
```

未来引入 ClickHouse 存 trace 明细，只新增 `storage/clickhouse/` 实现，业务代码零改动。

### 2.4 手写 DI（依赖注入）

不引入 Wire / Fx。`internal/app/app.go` 显式按顺序构造：日志 → 配置 → DB / Redis → 仓储 → 缓存 → 业务 service → handler → HTTP server。显式即文档，IDE 跳转友好，单测最容易写。

### 2.5 Stream 解析器选择

历史上曾通过 `internal/proxy/adapter/` 注册"每个 provider 一个完整 Adapter"。当前架构下，所有上游 **共用一个 pass-through 转发器**（上游 URL 完全由客户端在 baseURL 中指定），仅 SSE 解析器按上游 host 在 `stream` 包内部分流：

```go
// internal/proxy/stream/parser.go
// host 含 anthropic → anthropic_parser
// host 含 googleapis / generativelanguage → gemini_parser
// 其它 → openai_parser（OpenAI Chat Completions 是事实标准）
```

外部没有 "provider" 概念——trace 不打厂商标签，控制台也不按厂商筛选。

新增上游兼容只需：

1. 协议是 OpenAI Chat Completions 兼容（DeepSeek / Groq / Together / Moonshot / 本地 vLLM 等）：**不需要改代码**，客户端写完整 baseURL 即可。
2. 新协议族：在 `internal/proxy/stream/parser.go` 的 `NewParserForHost` 加 host 规则，并在 `internal/proxy/stream/` 新增解析器。

## 三、为什么不用 Kratos / Gin 等框架

| 项 | 框架方案 | 本项目（手搭） |
|---|---|---|
| 入门成本 | 学框架的设计哲学、Wire 用法、生命周期 | 只需会 Go 标准库 |
| 二进制大小 | Kratos 含大量你用不到的能力 | 极小 |
| 调试 | 中间件堆叠后行为难追踪 | 调用链直观 |
| 自由度 | 受框架抽象约束 | 100% 自主 |
| 升级风险 | 框架大版本 break change | 标准库基本零变化 |

手搭意味着自己要写 router 组装、middleware 链、错误处理、请求日志这些"框架本来包办的事"。但对于一个核心是反向代理 + 异步采集的项目，这些代码加起来不超过 300 行，**完全在可控范围内**。

## 四、Makefile 关键 Target

实际可用 Target（见仓库 `Makefile`）：

```makefile
make build          # 编译单二进制到 ./bin/ailens360
make run / make dev # 直接 `go run`（从 .env 读取配置）
make test           # go test ./...
make lint           # go vet ./...
make tidy           # go mod tidy
make clean          # 清理 bin/ dist/
make docker         # 构建 Docker 镜像
make docker-up      # docker compose up -d
make docker-down    # docker compose down
```

数据库迁移在程序启动时自动执行（`internal/storage/postgres/db.go`）；没有 sqlc 生成步骤，仓储实现走手写 SQL + `database/sql`。

## 五、配置

所有运行参数通过环境变量加载，无 yaml 配置文件：

```bash
cp .env.example .env
# 必填：
#   AILENS360_JWT_SECRET   （openssl rand -hex 32）
#   AILENS360_DB_DSN       （postgres://...）
#   AILENS360_REDIS_ADDR   （host:port）
```

完整字段定义见 `.env.example` 与 `internal/config/loader.go`。CI / 容器环境也可直接通过 `-e` / `EnvironmentFile=` 注入，绕过 `.env` 文件。

## 六、前端发布形态

前端**不内嵌**到 Go 二进制：

- `frontend/` 是独立 Vite 项目，`pnpm build` 输出 `frontend/dist/`
- Docker 镜像构建时会执行 `pnpm build`，并将 `frontend/dist/` 放入容器内，由 Go 进程直接托管
- 本地前端开发仍可使用 `pnpm dev`；此时跨域由 `AILENS360_API_CORS_ORIGINS` 控制

未来若希望恢复"单可执行文件 + 单配置"的发布形态，可通过 `embed.FS` 把 `frontend/dist/` 内嵌进 `cmd/ailens360`。当前保持分离是为了控制台前后端独立扩缩容。
