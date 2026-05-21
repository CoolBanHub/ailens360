# 项目目录结构

> 后端从零搭建，不使用 Kratos / Gin 等大型框架，只依赖 Go 标准库 + 少量轻量包。

## 零、已确认的技术决策

> 这里集中记录所有关键技术选型，作为后续开发的"宪法"。任何变更必须在此文档同步。

| # | 决策项 | 选择 | 理由摘要 |
|---|---|---|---|
| **D1** | Web 框架 | **不使用框架**，纯 stdlib + chi | 项目核心是反向代理 + 异步采集，框架带来的抽象多余；100% 自主控制 |
| **D2** | 进程拆分 | 三进程：`proxy` / `collector` / `api`，共享一个二进制 | 反代路径（SLA 敏感）与统计 / DB / 控制台彻底隔离；任一进程崩不影响其他 |
| **D3** | 进程间 IPC | Redis Stream + consumer group | 已有 Redis；最小新依赖；XAUTOCLAIM 天然防 stuck |
| **D4** | 路由库 | `go-chi/chi/v5` | 轻量、stdlib 兼容、中间件机制清晰、社区主流 |
| **D5** | DB 访问层 | **手写 SQL + `pgxpool`**，不引入 sqlc / ORM | 表结构稳定、查询不复杂；ORM 的好处覆盖不了它的隐式行为 |
| **D6** | trace 表存储 | Postgres RANGE 分区（按 created_at 月度）+ Go 维护作业 | 自动建分区、按 `RETENTION_MONTHS` 硬删超龄分区与对应 MinIO 对象；零 PG 扩展依赖 |
| **D7** | 大 body 存储 | S3 兼容对象存储（minio-go v7），PG 只存 object key | 主表瘦身、流式 multipart 上传支持大响应；fail-open 不影响代理 |
| **D8** | 依赖注入 | **手写构造函数**，不用 wire/fx | 项目规模不需要；显式即文档；IDE 跳转友好；测试最易写 |
| **D9** | 仓储模式 | 接口与实现分离 | 业务代码只依赖 `internal/storage/repo/` 接口，可平滑切换 Postgres → ClickHouse |
| **D10** | 缓存抽象 | L1 进程 LRU + L2 Redis + Pub/Sub 失效广播 | 多副本一致；业务读写无感 |
| **D11** | 日志 | `log/slog`（Go 1.21+ 标准库） | 标准化、结构化、零依赖 |
| **D12** | 配置 | 环境变量（`.env` 由 `godotenv` 加载） | 12-factor；不引入 yaml |

## 一、整体目录

```
ailens360/
├── cmd/
│   └── ailens360/
│       └── main.go                    # 入口分发器：proxy / collector / api 子命令
│
├── internal/                          # 内部代码，不对外暴露
│   ├── app/                           # 三进程组装
│   │   ├── common.go                  # 共享构造（pgxpool / redis / cache / bodystore）
│   │   ├── proxy.go                   # BuildProxy → ProxyApp.Run/Shutdown
│   │   ├── collector.go               # BuildCollector → CollectorApp（含 migrations、分区维护、消费者）
│   │   ├── api.go                     # BuildAPI → APIApp（REST + 静态 UI + pricing refresher）
│   │   └── ui.go                      # SPA fallback handler（api 进程用）
│   │
│   ├── proxy/                         # 【代理进程】
│   │   ├── handler.go                 # /<scheme>://<upstream> 路由 + 项目鉴权 + body 上传 + 透传
│   │   ├── sink.go                    # StreamSink：XADD event 到 Redis Stream
│   │   ├── intercept/                 # ResponseWriter 拦截器（同时写客户端 + 上传 + 解析）
│   │   └── stream/                    # SSE / chunked 流处理
│   │       ├── parser.go              # NewParserForHost(host) 按上游 host 选解析器
│   │       ├── openai_parser.go       # OpenAI 事件归一化
│   │       ├── anthropic_parser.go    # Anthropic 事件归一化
│   │       ├── gemini_parser.go       # Gemini 事件归一化
│   │       └── types.go               # stream.Event：trace 元数据 + body key/size
│   │
│   ├── collector/                     # 【消费者进程】
│   │   ├── consumer.go                # XREADGROUP 主循环 + XAUTOCLAIM reclaimer
│   │   └── pipeline.go                # Transformer：event → repo.Trace（tokenize/pricing/派生指标）
│   │
│   ├── partition/                     # 【消费者进程组件】Postgres 月度分区维护 goroutine
│   │
│   ├── api/                           # 【API 进程】控制台 API（给 Web Console 用）
│   │   ├── router.go                  # chi 路由组装
│   │   ├── middleware/                # CORS / JWT / 日志 / recover
│   │   ├── handler/
│   │   │   ├── handlers.go            # projects / traces / trace_groups / metrics/usage / body
│   │   │   └── auth.go                # /auth/login + /auth/me
│   │   └── response/                  # 统一响应格式
│   │
│   ├── bodystore/                     # S3 / MinIO 客户端（minio-go v7）
│   │   ├── store.go                   # UploadBytes / NewStreamingUploader / Get / PresignGet
│   │   └── key.go                     # {project}/{YYYYMM}/{trace}/{request|response}.{ext}
│   │
│   ├── auth/                          # 控制台 JWT 服务
│   ├── project/                       # Project CRUD + project_key resolver
│   ├── cache/                         # L1 LRU + L2 Redis + Pub/Sub 失效广播
│   ├── metrics/                       # Redis 实时计数器（滑动窗口 buckets）
│   ├── tokenizer/                     # tiktoken（OpenAI）+ Anthropic / Gemini 估算
│   ├── pricing/                       # 模型价格表 + 动态刷新（refresher）
│   ├── crypto/                        # AES-256-GCM keygen（dormant，运行时未启用）
│   ├── storage/
│   │   ├── repo/                      # 仓储接口定义（ProjectRepo / TraceRepo + 数据结构）
│   │   └── postgres/                  # Postgres 实现
│   │       ├── db.go                  # 连接 + 自动 apply migrations（仅 collector 进程调用）
│   │       ├── project_repo.go
│   │       ├── trace_repo.go          # CopyFrom 批量插入
│   │       └── migrations/            # 0001_init.up.sql（含分区表声明）
│   │
│   ├── config/                        # 配置结构体 + .env 加载，role-aware Validate
│   ├── logger/                        # slog 配置
│   └── version/                       # 编译期注入版本号
│
├── pkg/                               # 对外可复用的工具包
│   ├── shortid/                       # 短 ID 生成（base62）
│   └── sse/                           # SSE 通用解析工具
│
├── frontend/                          # React 前端（Vite；Docker 镜像会一并构建并由 api 进程托管）
│   ├── src/
│   │   ├── pages/
│   │   ├── components/                # BodyViewer（按需从 api 拉 body）等
│   │   ├── lib/                       # api client / types
│   │   └── i18n/
│   ├── package.json
│   └── vite.config.ts                 # dev proxy → http://127.0.0.1:8081（api 进程）
│
├── deploy/
│   └── docker/Dockerfile              # 单镜像；CMD 决定 role
│
├── docker-compose.yml                 # 全栈：3 进程 + Postgres + Redis + MinIO
├── docker-compose.deps.yml            # 仅依赖：Postgres + Redis + MinIO（本机 `make run` 用）
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

### 2.1 三进程拆分

代理路径承担产品的核心 SLA（LLM 流量"不能挂"）。把统计 / DB 写入 / REST 控制台拆到不同进程，能让任何一个挂掉都不波及代理：

- **proxy** `:8080`：反向代理 + SSE 解析 + body 上传 MinIO + XADD trace 元数据到 Redis Stream
- **collector** `:8082`（仅 /healthz）：XREADGROUP 消费 → tokenize/pricing → COPY 入 PG；后台 goroutine 跑分区维护
- **api** `:8081`：REST + 静态 UI + pricing refresher（从 models.dev 拉到 Redis warm key）

三个进程共享同一个二进制；命令行子命令决定角色。共享依赖：Postgres、Redis、MinIO/S3。

故障矩阵：

| 哪个挂 | 表现 |
|---|---|
| collector | proxy 继续服务、Stream 堆积、collector 重启后追上 |
| MinIO | proxy 继续服务、body_key 留空、UI 显示"正文未存档" |
| Redis | proxy 仍返回响应、trace 丢、project 缓存降级到 PG |
| api | 代理不受影响、控制台无法访问 |

### 2.2 internal vs pkg

- `internal/`：项目私有，不允许其他项目 import
- `pkg/`：通用工具（短 ID、SSE 解析等），可被未来的 SDK / CLI 复用

### 2.3 仓储模式（Repository Pattern）

`storage/repo/types.go` 只定义**接口** + 数据结构（`Project` / `Trace` / `TraceGroup`），具体实现放在 `storage/postgres/`。业务代码只依赖接口，**永远不直接 import 具体实现**。

未来引入 ClickHouse 存 trace 明细，只新增 `storage/clickhouse/` 实现，业务代码零改动。

### 2.4 手写 DI（依赖注入）

不引入 Wire / Fx。`internal/app/` 下每个 role 的 `Build*` 函数显式按顺序构造依赖：日志 → 配置 → DB / Redis / BodyStore → 仓储 → 缓存 → 业务 service → handler → HTTP server。共享构造（pgxpool / redis / project cache）抽到 `common.go` 复用。

显式即文档，IDE 跳转友好，单测最容易写。

### 2.5 Stream 解析器选择

所有上游 **共用一个 pass-through 转发器**（上游 URL 完全由客户端在 baseURL 中指定），仅 SSE 解析器按上游 host 在 `stream` 包内部分流：

```go
// internal/proxy/stream/parser.go
// host 含 anthropic → anthropic_parser
// host 含 googleapis / generativelanguage → gemini_parser
// 其它 → openai_parser（OpenAI Chat Completions 是事实标准）
```

外部没有 "provider" 概念——trace 不打厂商标签，控制台也不按厂商筛选。

新增上游兼容只需：

1. 协议是 OpenAI Chat Completions 兼容（DeepSeek / Grok / Together / Moonshot / 本地 vLLM 等）：**不需要改代码**，客户端写完整 baseURL 即可。
2. 新协议族：在 `internal/proxy/stream/parser.go` 的 `NewParserForHost` 加 host 规则，并在 `internal/proxy/stream/` 新增解析器。

### 2.6 trace 表自动分区

`traces` 表在 `0001_init.up.sql` 声明为 `PARTITION BY RANGE (created_at)`。collector 进程启动时跑 `internal/partition.Maintainer.Ensure(ctx)` 同步建当前月 + N 个未来月的分区，并在后台 goroutine 每 24h 重复执行。`AILENS360_PARTITION_RETENTION_MONTHS`（默认 3）控制超龄分区的硬删除：维护作业 DROP 对应 `traces_YYYYMM` 分区，并按 `{project_id}/{YYYYMM}/` 前缀清理 MinIO 对象，两者保持月度对齐。设为负数（如 `-1`）可关闭清理。

零 Postgres 扩展依赖，零外部 cron 依赖。

### 2.7 大 body 外置

`stream.Event` 不携带 request/response body 字节，只携带 `RequestBodyKey` / `ResponseBodyKey` / sizes。proxy 端：

- 请求 body：内存缓冲（受 `AILENS360_PROXY_MAX_REQUEST_BODY` 限制）→ goroutine 上传 MinIO
- 响应 body：边写客户端边写 MinIO multipart uploader（`internal/proxy/intercept` 的 `swallowingWriter` 防止 MinIO 报错阻塞客户端）

api 端取 body 走 `/api/traces/:id/body?part=X`，默认 api 流式转发（MinIO 可内网）；`AILENS360_BODY_STORE_PRESIGN_REDIRECT=true` 时 302 → 浏览器直拉 MinIO。

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
make build              # 编译单二进制到 ./bin/ailens360
make run / make dev     # 同时起三个进程（前台，Ctrl-C 一起停）
make run-proxy          # 仅 proxy
make run-collector      # 仅 collector
make run-api            # 仅 api
make test               # go test ./...
make lint               # go vet ./...
make tidy               # go mod tidy
make clean              # 清理 bin/ dist/
make docker             # 构建 Docker 镜像
make docker-up          # docker compose -f docker-compose.deps.yml up -d
make docker-down        # docker compose -f docker-compose.deps.yml down
```

数据库迁移在 **collector 启动时**自动执行（`internal/storage/postgres/db.go`）；proxy / api 启动不动 schema。没有 sqlc 生成步骤，仓储实现走手写 SQL + `pgxpool`。

## 五、配置

所有运行参数通过环境变量加载，无 yaml 配置文件：

```bash
cp .env.example .env
# 关键必填（按 role 不同）：
#   AILENS360_DB_DSN                          (三个进程都用)
#   AILENS360_REDIS_ADDR                      (三个进程都用)
#   AILENS360_BODY_STORE_ENDPOINT / _BUCKET / _ACCESS_KEY_ID / _SECRET_ACCESS_KEY
#                                             (三个进程都用)
#   AILENS360_JWT_SECRET                      (仅 api 进程必填)
```

`config.Validate(role)` 是 role-aware 的，每个进程只校验自己关心的字段，不会因为另一个 role 的可选项缺失而拒绝启动。

完整字段定义见 `.env.example` 与 `internal/config/loader.go`。CI / 容器环境也可直接通过 `-e` / `EnvironmentFile=` 注入，绕过 `.env` 文件。

## 六、前端发布形态

前端**不内嵌**到 Go 二进制：

- `frontend/` 是独立 Vite 项目，`pnpm build` 输出 `frontend/dist/`
- Docker 镜像构建时会执行 `pnpm build`，并将 `frontend/dist/` 放入容器内，由 **api 进程** 直接托管
- 本地前端开发仍可使用 `pnpm dev`（dev server 把 `/api` 反代到 `http://127.0.0.1:8081`）；此时跨域由 `AILENS360_API_CORS_ORIGINS` 控制

未来若希望恢复"单可执行文件 + 单配置"的发布形态，可通过 `embed.FS` 把 `frontend/dist/` 内嵌进 `cmd/ailens360`。当前保持分离是为了控制台前后端独立扩缩容。
