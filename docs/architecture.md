# 系统架构设计

> **基线**：Postgres + Redis + MinIO（或任意 S3 兼容对象存储）三套依赖。
> 应用由三个无状态进程组成 —— `proxy` / `collector` / `api`，共享同一个二进制。
> 任一进程崩溃不波及其他；多副本水平扩展按 role 独立。
> 项目尚未上线就直接选择了分布式就绪架构，避免后续返工，没有 "单机 SQLite" 路径。

## 一、整体架构

```
┌──────────────────────────────────────────────────────────────────┐
│                         用户应用层                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ Python   │  │ Node.js  │  │ Go       │  │ Java     │          │
│  │ App      │  │ App      │  │ App      │  │ App      │          │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘          │
│       └─────────────┴─────────────┴─────────────┘                 │
│                          │                                         │
│  baseURL = proxy_origin/{完整上游 URL}                              │
│  项目密钥 Header / Path / Query: sk-<64-char base62>              │
│  请求头  Authorization: <真实上游 Key，原样透传>                    │
└──────────────────────────┼──────────────────────────────────────┘
                           │  HTTP / SSE
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│   ailens360 proxy (:8080)  — LLM 流量入口，对外仅这一个端口        │
│   - 解析 /<scheme>://<upstream> 或 /<sk-...>/<scheme>://...       │
│   - Header / Path / Query project_key → project_id（L1 + L2）    │
│   - 透传 Authorization 到上游                                      │
│   - 按上游 host 选 SSE 解析器（openai / anthropic / gemini）        │
│   - 边读边写：[客户端] [MinIO multipart uploader] [parser pipe]    │
└──────────┬───────────────────────────────────────────────────────┘
           │ ① PUT body objects             │ ③ XADD compact event
           ▼                                ▼
   ┌───────────────┐                  ┌───────────────────────┐
   │ MinIO / S3    │ ◄────────────────│ Redis                 │
   │  bodies       │   presigned GET  │  - Stream IPC         │
   │  (内网/可选外  │                  │  - project cache L2   │
   │   网 + CORS)  │                  │  - realtime metrics   │
   └───────────────┘                  │  - pricing warm cache │
           ▲                          └────┬──────────────────┘
           │                               │ XREADGROUP
           │                               ▼
           │              ┌────────────────────────────────────┐
           │              │   ailens360 collector (:8082 hc)    │
           │              │   - 消费 Stream → batch transform   │
           │              │   - tokenize + pricing + cost       │
           │              │   - COPY into PG traces 分区表       │
           │              │   - 更新 realtime 指标               │
           │              │   - 后台 goroutine：分区维护         │
           │              │   - 启动时：apply migrations         │
           │              └─────────────┬──────────────────────┘
           │                            │ COPY
           │                            ▼
           │              ┌────────────────────────────────────┐
           │              │   PostgreSQL                        │
           │              │   - traces (RANGE PARTITION 月)     │
           │              │   - projects                        │
           │              └────────────┬───────────────────────┘
           │                           │ read
           │              ┌────────────▼───────────────────────┐
           │ stream bytes │   ailens360 api (:8081)            │
           └──────────────│   - REST /api/* + 静态 UI          │
                          │   - JWT 鉴权                        │
                          │   - GET /api/traces/:id/body       │
                          │     默认流式转发 MinIO 字节         │
                          │     PRESIGN_REDIRECT=true → 302    │
                          │   - pricing refresher → Redis       │
                          └────────────┬───────────────────────┘
                                       │
                                       ▼
                              ┌───────────────┐
                              │ Web Console   │
                              │ (React + TS)  │
                              └───────────────┘
```

**故障矩阵**：

| 哪个挂 | 表现 |
|---|---|
| collector | proxy 继续服务、Stream 堆积、collector 重启后追上 |
| MinIO | proxy 继续服务、body_key 留空、UI 显示"正文未存档" |
| Redis | proxy 仍返回响应、trace 丢、project 缓存降级到 PG |
| api | 代理不受影响、控制台无法访问 |

## 二、核心组件

### 2.1 Proxy 进程（反向代理）

**职责**：接收用户应用的请求，从 URL 路径中拆出**内嵌的完整上游 URL**，再从 Header / Path / Query 解析 Project，把请求**原样透传到客户端指定的上游**，同时把请求/响应正文上传到 MinIO、把 trace 元数据 XADD 到 Redis Stream。

**关键设计**：

- **URL-embedded upstream 路由**：代理流量形如 `/<scheme>://<完整上游 URL>` 或 `/<sk-...>/<scheme>://<完整上游 URL>`
  - 例：`/https://api.openai.com/v1/chat/completions`
  - 上游 URL 由**客户端在 baseURL 里直接指定**；AILens360 不持有任何 per-project 上游配置
  - 任何 OpenAI 兼容服务（DeepSeek / Groq / Together / Moonshot / 本地 vLLM / Ollama）都能直接接入
- **手动路径解析（chi catch-all）**：因为内嵌 URL 含 `://` 这种结构，chi 路由的 `{var}` 捕获会把路径段里的连续 `/` 折叠 / 归一化，导致 scheme 信息丢失。代理因此使用 `/*` 通配挂载（更具体的 `/healthz` 先匹配），再在 `parseProxyPath` 中**手动解析 `r.URL.Path`**（Go 的 `net/http` 会在 `r.URL.Path` 上保留原始的 `//`），剥掉前导 `/` 后校验 `http://` / `https://` 前缀即可。实现见 `internal/proxy/handler.go`。
- **Project 解析**：
  - 项目身份支持 `X-AILens-Project-Key`、`/<sk-...>/` 路径前缀、`?sk=<sk-...>` 查询参数；缺失密钥返回 401 `missing_project_key`
  - `project_key` 在创建 Project 时生成（`sk-` + 64 位 base62，约 381 bits 熵），控制台支持「重置」轮换；轮换会同步从缓存 evict 旧 key 并广播
  - 三层缓存：进程内 LRU（L1）→ Redis（L2）→ Postgres（L3），Project 写路径 evict 时通过 Redis Pub/Sub 广播到其他副本
  - Project 仅承载归因与统计隔离，不保存上游 / 真实 API Key / baseURL
- **SSE 解析器按 host 在内部选择**：`stream.NewParserForHost(upstream.Host)`
  - host 含 `anthropic` → Anthropic 解析器
  - host 含 `googleapis` 或 `generativelanguage` → Gemini 解析器
  - 其他一律 → OpenAI 解析器（默认，覆盖所有 OpenAI Chat Completions 兼容上游）
  - **仅影响**流解析路径，不出现在 trace、API 或控制台。**转发目标 URL 完全由客户端 URL 中的内嵌上游决定**。
- **统一 pass-through 转发**：所有上游共用一个透传转发器
  - 上游特有的请求头（如 Anthropic 的 `anthropic-version`）由客户端 SDK 自己设置，代理不注入
- **Authorization 透传**：客户端的 `Authorization` / `x-api-key` / `x-goog-api-key` 原样透传给上游；AILens360 **不持有、不替换、不存储**真实上游 Key
- **正文上传到 MinIO**：
  - 请求 body：内存缓冲（受 `AILENS360_PROXY_MAX_REQUEST_BODY` 限制；`.env.example` 为 10 MiB）→ 并行 goroutine 上传 MinIO
  - 响应 body：边写客户端边写 MinIO multipart uploader；`swallowingWriter` 包一层防止 MinIO 卡住客户端
  - 上传失败 → fail-open：客户端响应正常，event 中 body_key 留空
- **XADD trace 元数据**：响应结束后把压缩 JSON event（含 body 对象 key + size + 元数据，不含 body 字节）XADD 到 `ailens360:traces` stream。失败也只记日志（已经返回客户端了）
- **不直接写 PG**：proxy 进程**不接触** PG `traces` 表，DB 慢查询 / 锁不会影响代理路径
- **敏感 Header REDACT**：event 内的 header map 在序列化前对 `Authorization` / `Cookie` / `x-api-key` / `x-goog-api-key` 统一替换为 `[redacted]`，保证真实 Key 不进入 Stream、不进入 PG、不进入 API 响应

### 2.2 Stream Parser（流式解析器）

**职责**：解析 SSE / chunked 流，**逐 chunk 打时间戳**，提取 token、增量内容、结束原因，并实时计算流式指标。

#### 2.2.1 关键时间戳

每次代理请求都在以下事件点打时间戳（单位：纳秒精度，统一用 `time.Now()`）：

| 时间戳 | 含义 |
|---|---|
| `t_request_in` | Proxy 接收到客户端请求的时刻 |
| `t_upstream_request_sent` | 请求 body 完整写入上游的时刻 |
| `t_upstream_first_byte` | 收到上游返回的第一个字节（HTTP TTFB） |
| `t_first_token` | 解析出第一个真正的 token delta 的时刻（SSE TTFT） |
| `t_chunk[i]` | 每个 chunk 到达的时刻数组 |
| `t_last_token` | 解析出最后一个 token delta 的时刻 |
| `t_response_out` | 完整响应写回客户端的时刻 |

#### 2.2.2 派生指标（核心 KPI）

| 指标 | 计算方式 | 说明 |
|---|---|---|
| `ttfb_ms` | `t_upstream_first_byte - t_upstream_request_sent` | 上游 HTTP 首字节延迟（含网络） |
| **`ttft_ms`** | `t_first_token - t_request_in` | **首 token 端到端延迟**（核心 KPI） |
| `latency_ms` | `t_response_out - t_request_in` | 总耗时 |
| `gen_duration_ms` | `t_last_token - t_first_token` | 纯生成时长（不含 prefill） |
| `tps` | `output_tokens / gen_duration_ms × 1000` | 输出吞吐（tokens/sec） |
| `chunk_count` | `len(t_chunk)` | 总 chunk 数 |
| `bytes_streamed` | sum(chunk size) | 流总字节数 |

#### 2.2.3 Stream 状态机

每个 SSE 流被建模为状态机，终态记入 `traces.stream_status`：

```
[Connecting] → [WaitingFirstByte] → [WaitingFirstToken]
                                          │
                                          ▼
                                    [Streaming] ←──┐
                                          │         │ 每个 chunk
                                          ├─────────┘
                                          │
              ┌───────────────────────────┼───────────────────────┐
              ▼                           ▼                       ▼
        [Completed]                  [Aborted]               [Errored]
   (finish_reason 收到)         (客户端断开 / context 取消)  (上游报错)
```

#### 2.2.4 Provider 协议适配

每个 Parser 把原生事件**归一化**为内部 `StreamEvent`（见 `internal/proxy/stream/types.go`）：

**OpenAI**：
- SSE 格式 `data: {...}\n\n`，最后一行 `data: [DONE]`
- usage 字段需要请求时设置 `stream_options.include_usage=true`，AILens360 自动注入
- 上游未给 usage 时，用本地 tokenizer 估算 output_tokens 并标记 `tokens_estimated=true`

**Anthropic**：
- SSE 事件类型化：`message_start` → `content_block_start` → `content_block_delta`(*) → `content_block_stop` → `message_delta` → `message_stop`
- usage 在 `message_start`（input）和 `message_delta`（output cumulative）里
- TTFT = 收到第一个 `content_block_delta` 的时间

**Gemini**：
- SSE 格式 `data: {...}\n\n`
- `usageMetadata` 在最后一个 chunk
- TTFT = 收到第一个含 `text` 的 candidate 的时间

#### 2.2.5 异常与边界情况

| 情况 | 处理 |
|---|---|
| 客户端中途断开 | 状态置为 `aborted`，已收到的 chunk 仍然落库 |
| 上游报错（HTTP 5xx） | 记录错误码、错误体；`ttft_ms` 为 NULL |
| 收到 `[DONE]` 但无 usage | 用 tokenizer 估算 output_tokens，`tokens_estimated=true` |
| 非 stream 请求 | 跳过 chunk 时间戳，`ttft_ms = NULL`，只记录 `ttfb_ms` 与 `latency_ms` |
| 请求 body 超 `AILENS360_PROXY_MAX_REQUEST_BODY` | 走 `http.MaxBytesReader` 拦截，返回 413（`.env.example` 为 10 MiB；内置默认 32 MiB） |
| MinIO 上传失败 | body_key 留空，trace 仍写入；UI 显示"正文未存档" |

### 2.3 Collector 进程

**职责**：消费 Redis Stream `ailens360:traces`，把 event → `repo.Trace`，COPY 批量入 PG，维护实时指标，跑分区维护。

**Pipeline 设计**（XREADGROUP → batch → COPY，不再走 channel）：

```
Redis Stream "ailens360:traces"
       │ XREADGROUP BLOCK 5s COUNT 200
       ▼
┌──────────────────────────┐
│  Consumer  (主循环)        │
│  - decode JSON event     │
│  - Transformer.Transform │ ← tokenize + pricing + 派生指标
│  - batch [*repo.Trace]   │
└────────────┬─────────────┘
             ▼
   ┌──────────────────┐         ┌──────────────────┐
   │ TraceRepo COPY    │ ──────► │ PG traces (分区)  │
   │ BatchCreate       │         └──────────────────┘
   └────────┬─────────┘
            ▼
   ┌──────────────────┐         ┌──────────────────┐
   │ Realtime.Record   │ ──────► │ Redis 计数器     │
   │ Batch             │         └──────────────────┘
   └────────┬─────────┘
            │ XACK ids
            ▼
       ✓ ack
```

**消费组**：

- 组名：`collector`（可配，但通常一个集群一个组）
- consumer name：`${HOSTNAME}-${PID}` —— 多副本天然分片
- **PEL reclaimer**：后台 goroutine 每 30s 跑一次 `XAUTOCLAIM`，把 idle 超过 `PENDING_IDLE_MAX`（默认 60s）的消息从死掉的副本搬过来；防止 collector 副本崩溃造成消息卡死

**核心指标分类**：

- **Token**：input / output / cached_input / cache_creation_input / reasoning / total，按 `internal/pricing` 价格表计算成本
- **延迟**：ttfb / **ttft** / latency / gen_duration
- **吞吐**：tps（tokens/sec）、bytes_streamed
- **质量**：finish_reason 分布、错误率、stream_status（completed / aborted / errored / stalled）
- **业务**：按 project / model / user / session / tag 多维度聚合

**实时指标**：Redis 维护 QPS、最近 1/5/15 分钟的 ttft、错误率、tokens/秒、各 Project 计数；Web Console 长窗口查询直接读预聚合。

**Token 估算策略**：当上游未返回 usage 时，collector 用 ResponseText（随 event 一起 XADD 过来）通过 tokenizer 估算 output_tokens。input 估算被砍掉了 —— request body 字节不进 Stream，无法在 collector 侧重算；现代上游基本都返回 usage，这个 fallback 罕用。

**分区维护**：`internal/partition.Maintainer` 在 collector 启动时同步建当前月 + N 个未来月分区（默认 N=1），并启动 24h 周期 goroutine 重复执行。`AILENS360_PARTITION_RETENTION_MONTHS`（默认 3）控制硬删除：维护作业 DROP 超龄 `traces_YYYYMM` 分区，并按 `{project_id}/{YYYYMM}/` 前缀同步清理 MinIO 对象，两个存储保持月度对齐；设为负数则关闭清理。

**Migrations 所有权**：collector 启动时跑 `postgres.Migrate(ctx, pool)`。proxy / api 启动不动 schema，避免多进程并发跑迁移。

### 2.4 Storage Layer

存储层职责：

- **PostgreSQL**：trace 元数据（按月分区）+ project 配置
- **Redis**：project 缓存 L2 + Pub/Sub 失效广播 + 实时指标 + Stream IPC + pricing 暖缓存
- **MinIO/S3**：请求 / 响应正文，PG 主表只存对象 key

详见 [§五 数据模型](#五数据模型)。

### 2.5 API 进程

**协议**：REST（v0.1）；WebSocket 实时推送规划在 v0.2+。

**v0.1 已挂载的路由**（见 `internal/api/router.go`，监听 `:8081`）：

- `POST /api/auth/login` —— 用户名/密码换取 JWT
- `GET  /api/auth/me` —— 当前登录态
- `POST/GET/PUT/DELETE /api/projects[/{id}]` —— Project CRUD
- `GET  /api/traces` —— Span 列表（支持 `project_id` / `trace_id` / `user_id` / `session_id` / `model` / `status` / 时间范围过滤）
- `GET  /api/traces/{id}` —— Span 详情（不含 body）
- `GET  /api/traces/{id}/body?part=request|response` —— body 字节
  - 默认：api 从 MinIO 拉字节流式转发给浏览器，MinIO 可内网
  - `AILENS360_BODY_STORE_PRESIGN_REDIRECT=true`：返回 302 → 浏览器直拉 MinIO（要求 MinIO 对外可达 + CORS）
- `GET  /api/trace_groups` —— Langfuse 风格逻辑 trace 聚合（按 `trace_id` 收敛）
- `GET  /api/metrics/usage` —— 用量聚合（维度：model / project / day / hour）
- `GET  /healthz` / `GET /version`

**额外职责**：

- **pricing.Refresher**：定期从 models.dev 拉价格表写到 Redis 暖缓存 `pricing:models.dev:v1`；collector 进程从此 key 读
- **静态 UI 托管**：`frontend/dist` 由 SPA fallback handler 提供

**规划中、尚未挂载**：`POST /api/traces/{id}/replay`、`WS /api/stream`、OTLP receiver。

详细接口契约见 [api-design.md](./api-design.md)。

### 2.6 Web Console

**技术栈**：React 18 + TypeScript + Vite + Tailwind + HeroUI（位于 `frontend/`，**独立 Vite 项目，不内嵌到 Go 二进制**，发布时单独构建）。

**v0.1 实际页面**（见 `frontend/src/pages/`）：

1. `/login` —— 控制台登录
2. `/` —— 项目列表与新建
3. `/project/:id/overview` —— 项目维度概览大盘
4. `/project/:id/setup` —— 复制 `proxy_prefix` 与开箱即用 baseURL 示例
5. `/project/:id/traces` —— Trace 列表与过滤
6. `/project/:id/trace/:traceId` —— Trace 详情（请求 / 响应 / 流式 chunk / 时间线）
7. `/project/:id/settings` —— 项目设置

## 三、数据流

### 3.1 请求路径（proxy 进程内 + 跨进程）

```
1. 用户应用发起 POST http://proxy.host/https://api.openai.com/v1/chat/completions
   Header: Authorization: Bearer sk-real-openai-key
           X-AILens-Project-Key: sk-<64-char base62>
	   # 或使用 /sk-.../ 路径前缀 / ?sk=sk-... 查询参数

2. proxy 进程接收（chi catch-all 路由 + healthz 例外）：
   parseProxyPath 剥掉前导 "/" 并校验 http:// 或 https:// 前缀
   → upstream_url = "https://api.openai.com/v1/chat/completions"
   (Go net/http 保留原始 "//"，scheme 不丢)

3. 从 Header / Path / Query 读 project_key，查三层缓存 (LRU → Redis → Postgres)
   → 拿到 project_id；缺失密钥返回 401，未命中返回 404

4. 按 upstream.Host 在 stream 包内部选解析器
   (api.openai.com → OpenAI 解析器)

5. 改写出站请求：
   - URL: 直接使用 upstream_url
   - Authorization: 原样透传
   - 删除所有 X-AILens-* 内部 Header

6. 读取完整请求 body（受 `AILENS360_PROXY_MAX_REQUEST_BODY` 限制）
   → 启 goroutine: PUT 到 MinIO，object key = {project}/{YYYYMM}/{trace_id}/request.json

7. 透传请求到上游，上游开始返回响应

8. proxy 用 io.MultiWriter 同时写三处：
   a. http.ResponseWriter        (客户端，最优先)
   b. bodystore streamingUploader (MinIO multipart 上传)
   c. parser io.PipeWriter       (按上游 host 选的 SSE 解析器)
   swallowingWriter 包了 (b) — MinIO 卡了不会拖累 (a)

9. 流结束后 / 非流响应读完后：
   - 关闭 multipart uploader 等 MinIO 完成
   - 等步骤 6 的请求 body 上传 goroutine 完成
   - Parser.Finalize 把 token / finish_reason 等填进 stream.Event

10. 在 event 中 REDACT 敏感 header（Authorization / Cookie / x-api-key / X-AILens-Project-Key）
    → marshal JSON → XADD ailens360:traces VALUES data=<json>
    （事件载荷不含 body 字节，只含 body_key + size + ResponseText 截断版）

11. 客户端连接已经独立完成，无须等 XADD

跨进程：
12. collector XREADGROUP 拉到上面那条 event
13. Transformer 算 token (用 ResponseText) / cost / 派生指标
14. batch 累积到 200 条或下次 read 没拿到新消息时：
    - TraceRepo.BatchCreate → COPY into traces 分区表
    - Realtime.RecordBatch → Redis 计数器
    - XACK 这批 message id
```

### 3.2 查询路径

```
1. 用户打开 Web Console（api 进程托管，:8081）
2. React 调用 REST API：GET /api/traces?project_id=proj_xxx&limit=50
3. api 查 PG → 返回 trace 列表（不含 body）

4. 用户点击某条 trace
5. React 调用 GET /api/traces/:id
6. api 读取 span 元数据（敏感 Header 已 REDACT）

7. React 渲染详情 + 触发 GET /api/traces/:id/body?part=request|response
8. api：
   - 默认模式：bodystore.Get → io.Copy 到 response writer，转发 Content-Encoding: gzip
   - PRESIGN 模式：bodystore.PresignGet → 302 Location: <presigned-minio-url>
9. 浏览器拿到 body 字节（gzip 自动解码），喂给 ChatViewer 渲染
```

## 四、安全模型

### 4.1 核心边界

AILens360 位于用户应用和上游 LLM 服务之间，会接触客户端透传的 Authorization、prompt、completion 和调用元数据。默认安全目标：

1. **不持有、不存储真实上游 API Key**：客户端的 `Authorization` / `x-api-key` / `x-goog-api-key` 仅在内存中透传给上游，**不写入数据库**、**不进入日志**、**不出现在控制台 API 响应**。
2. Project 的 `project_key` 是入口凭据（视同密钥），默认 `sk-` + 64 位 base62（约 381 bits 熵），密码学上不可被穷举枚举；生产环境必须强制 HTTPS 防止传输层泄露。Header 模式最不容易进入访问日志，Path / Query 模式需要在网关日志中脱敏。客户端透传的上游 API Key 是上游侧的安全边界，不在 AILens360 持有范围内。
3. 请求 / 响应原文默认可观测，但敏感 Header 强制 REDACT。

### 4.2 上游 API Key —— 透传，不持有

- AILens360 **不存储**上游真实 API Key；客户端在每次请求的 `Authorization` / `x-api-key` / `x-goog-api-key` Header 中提供。
- Proxy 路径只在内存中读取这些 Header 并直接透传给上游，落库前会被替换为 `[redacted]`。
- 控制台 API 返回 trace 详情时，敏感 Header 同样为 `[redacted]`，避免任何旁路泄露。
- 即便 AILens360 数据库被拷走，**没有任何上游 Key 会泄露**。

> 这是相对历史方案（集中加密保存上游 Key）的主动收缩：放弃"集中保管 + 一键撤销"的便利，换取"AILens360 永远不是 Key 失窃的攻击面"。
>
> 代价：失去"在 AILens360 一键撤销访问"的能力——要换 Key 必须去 OpenAI / Anthropic 后台轮换真实 Key。

REDACT 实现位置：`internal/proxy/handler.go:cloneHeader`。重放接口（规划中）落地时要求调用方在 body 中重新提供 `authorization`，因为原 Header 不可恢复。

### 4.3 控制台鉴权

v0.1 采用**单管理员 + JWT** 模型：

- 用户名 / 密码来自 `AILENS360_AUTH_USERNAME` / `AILENS360_AUTH_PASSWORD`，默认 `admin` / `admin`，**生产前必须改掉**。
- `POST /api/auth/login` 校验后颁发 JWT，签名密钥由 `AILENS360_JWT_SECRET` 控制；**留空则启动失败**（避免重启后会话作废这种隐式行为）。
- 所有 `/api/*`（除 `/auth/login`）走 `middleware.AdminJWT` 校验。
- JWT 不能用于代理上游 LLM 调用，与代理路径完全隔离。

**规划（v0.3）**：多用户 + RBAC（Owner / Admin / Viewer）+ 控制台 API Key（`ail-ak-...` 形态，作用域 / 过期 / 审计）。

### 4.4 数据采集与脱敏

| 数据 | 当前行为 |
|---|---|
| 请求 header | 全量保存到 PG；`Authorization` / `Cookie` / `x-api-key` / `x-goog-api-key` / `X-AILens-Project-Key` 在写入 Stream 之前强制 REDACT 为 `[redacted]` |
| 请求 body | 上传到 MinIO（受 `AILENS360_PROXY_MAX_REQUEST_BODY` 限制；超限返回 413） |
| 响应 body | 流式上传到 MinIO（multipart）；MinIO 失败 → fail-open，body_key 留空 |
| ResponseText | 解析后的纯文本（用于 token 估算），随 event 经 Redis Stream 流转；同样受截断 |
| prompt/completion 脱敏 | **未实现**：自动脱敏手机号 / 邮箱等在路线图 |

PG `traces` 表**完全不存 body 字节**，只保留 body 在对象存储中的 key + size；body 的真正持久层是 MinIO/S3。客户端透传的上游 Authorization 已被 REDACT，绝不在 trace 详情、trace 列表 API、Web Console 中以明文出现。

### 4.5 数据保留与日志

- trace 明细按月分区；`AILENS360_PARTITION_RETENTION_MONTHS`（默认 3）控制硬删除窗口，超龄分区直接 DROP，无需运维介入
- body 对象同步由 collector 维护作业清理（按 `{project_id}/{YYYYMM}/` 前缀），与 PG 分区月度对齐；若要保留 PG 但提早回收 MinIO，可在 bucket 上叠加自带 Lifecycle policy
- 日志不输出客户端透传的 Authorization / x-api-key / x-goog-api-key 明文；上游错误体进 trace 前必须按规则脱敏。
- 代理返回给客户端的错误保持 SDK 兼容，但不暴露内部配置 / DB 错误细节。

## 五、数据模型

> Schema 以 `internal/storage/postgres/migrations/0001_init.up.sql` 为准；运行时由启动逻辑自动 apply。

### 5.1 设计原则

1. **一级实体只有两张表**：`projects` 与 `traces`（后者是分区表，子分区 `traces_YYYYMM` 自动生成）。
2. **Trace 即 Span**：每一次代理转发的上游 HTTP 调用对应 `traces` 表中一行。多行通过 `trace_id` 聚合成一次"逻辑 trace"（Langfuse 语义），便于多步 Agent 场景的链路视图。
3. **敏感 Header 入库前 REDACT**：见 §4.2。
4. **请求 / 响应正文外置**：bodies 存 MinIO/S3，PG 只保留对象 key + size，主表瘦身、备份变小。
5. **时间戳统一为 Unix 毫秒**（traces）/ Unix 秒（projects），`BIGINT` 存储，避免驱动层时区问题。
6. **按月分区 + Go 维护作业**：`traces` `PARTITION BY RANGE (created_at)`，collector 进程同时建当前月 + 未来月分区；`RETENTION_MONTHS`（默认 3）控制硬删除，DROP 超龄分区并同步清理 MinIO 对应前缀。

### 5.2 `projects`

唯一的一级租户实体。一个 Project 拥有一个全局唯一的 `project_key`，作为客户端 Header / Path / Query 中的项目凭据，**视同密钥**。

| 列 | 类型 | 约束 | 含义 |
|---|---|---|---|
| `id` | TEXT | PK | 内部主键，ULID |
| `project_key` | TEXT | NOT NULL UNIQUE | `sk-` + 64 位 base62，客户端凭据；可通过控制台重置 |
| `name` | TEXT | NOT NULL UNIQUE | 人类可读名字 |
| `created_at` | BIGINT | NOT NULL | Unix 秒 |
| `updated_at` | BIGINT | NOT NULL | Unix 秒 |

索引：`idx_projects_project_key ON (project_key)`。

**生命周期约束**：Project 删除后 `project_key` 永不复用；控制台「重置 project_key」会写入新值并把旧值从缓存 evict 掉，同时通过 Pub/Sub 广播到其他副本（`internal/project/resolver.go`）。

### 5.3 `traces`（分区表）

每次代理转发到上游的 HTTP 调用一行。语义上是 **logical trace 的一个 span**——多行通过 `trace_id` 聚合。

声明为 `PARTITION BY RANGE (created_at)`；子分区按月（`traces_YYYYMM`）由 collector 进程的分区维护器自动建。PK 必须包含分区键，因此为 `(id, created_at)`。

#### 标识与归属

| 列 | 类型 | 含义 |
|---|---|---|
| `id` | TEXT | Span ID（PK 一部分，与 `created_at` 组合） |
| `trace_id` | TEXT | 逻辑 trace ID。来自 `X-AILens-Trace-Id`；未指定时与 `id` 相等 |
| `trace_name` | TEXT | 逻辑 trace 的人类标签，来自 `X-AILens-Trace-Name` |
| `project_id` | TEXT NOT NULL | 关联 `projects.id` |
| `user_id` | TEXT | 来自 `X-AILens-User` |
| `session_id` | TEXT | 来自 `X-AILens-Session` |
| `tags` | TEXT | 来自 `X-AILens-Tag`，逗号分隔 |

#### 路由与协议

| 列 | 类型 | 含义 |
|---|---|---|
| `model` | TEXT | 请求 body 中识别出的 `model` 字段 |
| `is_stream` | BOOLEAN | 是否流式 |
| `request_path` | TEXT | 上游**绝对** URL（含 scheme + host + path + query） |

#### 状态与错误

| 列 | 类型 | 含义 |
|---|---|---|
| `status` | TEXT | `success` / `error` / `aborted` |
| `status_code` | INTEGER | 上游返回 HTTP 状态码 |
| `error_message` | TEXT | 错误简述 |
| `finish_reason` | TEXT | LLM 完成原因（`stop` / `length` / ...） |
| `stream_status` | TEXT | 流状态机末态：`completed` / `aborted` / `errored` / `stalled` |

#### 请求 / 响应正文（外置 + 元数据）

正文字节存对象存储；这里只存 key + size。空 `*_body_key` 表示上传失败或被跳过。

| 列 | 类型 | 含义 |
|---|---|---|
| `request_headers` | TEXT | JSON 字符串，敏感 Header 已 REDACT |
| `response_headers` | TEXT | JSON 字符串 |
| `request_body_key` | TEXT | MinIO/S3 对象 key，例如 `proj_xxx/202605/tr_yyy/request.json` |
| `response_body_key` | TEXT | 同上 |
| `request_body_size` | BIGINT | 上传到对象存储的字节数（gzip 前的原始字节） |
| `response_body_size` | BIGINT | 同上 |
| `timeline` | TEXT | 关键事件时间戳 JSON 数组 |

#### Token 与成本

四个输入相关计数列互不重叠（disjoint），每列按各自费率计费，求和即"总输入计费单位"。Parser 层会把 OpenAI / Gemini 上游回传的 `prompt_tokens`（含 cached）减去 cached 后再写入 `input_tokens`，与 Anthropic 原生语义对齐。

| 列 | 类型 | 含义 |
|---|---|---|
| `input_tokens` | INTEGER | **未命中**缓存的输入 token，按 input 费率计费 |
| `cached_input_tokens` | INTEGER | 命中缓存的输入 token，按 cache_read 费率计费（缺失时退回 input 费率） |
| `cache_creation_input_tokens` | INTEGER | cache-write token，按 cache_write 费率计费（目前仅 Anthropic 上报） |
| `output_tokens` | INTEGER | 输出 token |
| `total_tokens` | INTEGER | 上游回传的 `total_tokens`；缺失时由 parser 按 `input + cached + cache_creation + output` 求和 |
| `reasoning_tokens` | INTEGER | thinking / reasoning token（OpenAI o-series、Gemini thoughts） |
| `tokens_estimated` | BOOLEAN | true = 由本地 tokenizer 估算（上游未给 usage） |
| `cost_usd` | DOUBLE PRECISION | `input·r_in + cached·r_cr + cache_creation·r_cw + output·r_out`，超 tier 阈值时整组费率切到 tier |

#### 延迟与吞吐

| 列 | 类型 | 含义 |
|---|---|---|
| `latency_ms` | BIGINT | 总耗时 |
| `ttft_ms` | BIGINT NULL | 首 token 端到端延迟，非流式为 NULL |
| `ttfb_ms` | BIGINT NULL | 上游 HTTP 首字节延迟 |
| `gen_duration_ms` | BIGINT NULL | 纯生成时长 |
| `tps` | DOUBLE PRECISION | 输出 tokens/sec |
| `chunk_count` | INTEGER | SSE chunk 总数 |
| `bytes_streamed` | BIGINT | 流总字节数 |
| `created_at` | BIGINT NOT NULL | Unix 毫秒 |

#### 索引

声明在父表上的分区索引（PG 12+ 自动传播到子分区）：

| 索引 | 列 | 用途 |
|---|---|---|
| `idx_traces_project_created` | `(project_id, created_at DESC)` | Project 维度列表 |
| `idx_traces_model` | `(model, created_at DESC)` | 模型过滤 |
| `idx_traces_status` | `(status)` | 错误率聚合 |
| `idx_traces_user_created` | `(user_id, created_at DESC)` | 按业务用户下钻 |
| `idx_traces_session_created` | `(session_id, created_at DESC)` | 按会话聚合 |
| `idx_traces_trace_id_created` | `(trace_id, created_at ASC)` | 逻辑 trace 内 span 升序展开 |

PK `(id, created_at)` 自动是分区索引；`GetByID(id)` 不带 `created_at` 时无法做分区裁剪，会扫所有子分区，但每个子分区都有 PK 索引，trace 详情访问可接受。

### 5.4 视图层逻辑：TraceGroup

`TraceGroup` 不是物理表，而是 `internal/storage/repo/types.go` 中定义的逻辑视图——按 `trace_id` 在查询时聚合：

- `span_count`：组内 span 数
- `input_tokens / output_tokens / total_tokens / cost_usd`：组内求和
- `latency_ms`：组内 `max(created_at) − min(created_at)`
- `status`：组内"最坏态"（`error > aborted > success`）

`GET /api/trace_groups` 返回该视图；`GET /api/traces` 仍返回 span 列表，可通过 `trace_id` 过滤展开单组。

### 5.5 路线图中的表（尚未落地）

- `metrics_minutely` / `metrics_hourly` / `metrics_daily`：预聚合指标
- `users` / `teams` / `memberships`：多用户与 RBAC（v0.3）
- `alerts` / `alert_rules`：告警引擎（v0.3）
- `audit_logs`：控制台审计（v0.3）

新增迁移保持单向：`{n}_xxx.up.sql` + `{n}_xxx.down.sql` 一一对应，新增字段优先用 `DEFAULT` 兼容历史行。

## 六、技术选型

### 6.1 后端：Go + 标准库为主，零框架

**核心原则**：**不引入大型 Web 框架**（Kratos / Gin 等），从零搭建系统结构。理由：

- 项目核心是反向代理 + 异步采集，本质上不是典型 CRUD 微服务，框架带来的抽象多余
- 单二进制要小，依赖少
- 控制力 100%，避免被框架绑架（事件、生命周期、中间件机制）

**技术栈**（实际依赖见 `go.mod`）：

| 用途 | 选型 | 理由 |
|---|---|---|
| HTTP 路由 | `go-chi/chi/v5` | 轻量、stdlib 兼容、中间件机制清晰 |
| 反向代理 | `net/http` + io.MultiWriter | 自己写 ~200 行，避免 `httputil.ReverseProxy` 对响应体的二次包装；流式分流给 client/MinIO/parser 更直观 |
| 对象存储客户端 | `minio/minio-go/v7` | 轻量，AWS S3 / MinIO 全兼容 |
| Stream IPC | `redis/go-redis/v9` 的 XADD / XREADGROUP / XAUTOCLAIM | 无新中间件依赖 |
| 日志 | `log/slog` | Go 1.21+ 标准库，结构化日志 |
| 配置 | 环境变量（`godotenv` 加载 `.env`） | 无 yaml，符合 12-factor |
| Postgres 驱动 | `jackc/pgx/v5` | 性能与功能 Go 生态最佳 |
| 数据访问 | 手写 SQL + `pgxpool` | 没引入 sqlc / ORM；表结构稳定，复杂度可控 |
| Redis 客户端 | `redis/go-redis/v9` | 主流、与 pgx 风格一致 |
| 缓存 | `hashicorp/golang-lru/v2` + Redis | L1 进程 LRU + L2 Redis + Pub/Sub 失效广播 |
| JWT | `golang-jwt/jwt/v5` | 控制台鉴权 |
| Tokenizer | `pkoukk/tiktoken-go` | OpenAI tiktoken 的 Go 移植 |
| 依赖注入 | **手写构造函数**，不用 wire | 项目规模不需要，构造函数显式即文档 |

**项目结构**：详见 [project-structure.md](./project-structure.md)。

### 6.2 前端：React + TypeScript

- React 18 + TypeScript + Vite + Tailwind + HeroUI
- TanStack Query 管远程数据，本地 state 用原生 hooks
- 与后端的 API client 集中在 `frontend/src/lib/api.ts`

## 七、关键设计决策

### 7.1 为什么用反向代理而不是 SDK？

| 维度 | SDK 方案 | 反向代理方案 |
|---|---|---|
| 集成成本 | 每种语言一个 SDK | 改 baseURL 即可 |
| 维护成本 | N 倍工作量 | 1 倍 |
| 兼容性 | 受 SDK 版本制约 | HTTP 协议天然兼容 |
| 性能开销 | 进程内 | 网络一跳（~1ms） |

放弃 1ms 的延迟换取 100 倍的集成便利，对绝大多数场景是好交易。

### 7.2 为什么 Project 直挂 baseURL，而不是引入 upstream 配置层？

> 历史上 AILens360 曾有过四个一级概念：`Project / Upstream / ShortLink / ProxyKey`，分别承载"项目归因 / 上游 provider 与真实 Key / 入口短链 / 应用侧鉴权"。早期重构合并为单一 Project。

对比过两种代理模型：

**方案 A：完整 API 网关**（已放弃）—— 控制台保存加密的真实 Key，颁发独立 ProxyKey 给应用。优点是真实 Key 集中保管、可一键停用；缺点是用户接入要走"创建上游 → 创建短链 → 颁发 Proxy Key → 写主密钥"四步，且 KMS / Master Key 的运维负担与"轻量可观测工具"定位相悖。

**方案 B：Project + URL 内嵌上游 + 项目密钥 + Authorization 透传**（采用）—— 控制台只创建 Project，应用把 `proxy_prefix + 完整上游 URL` 当 baseURL，原 Authorization 不变，项目身份走 Header / Path / Query。优点：

- 接入成本与 Langfuse 持平：创建 Project → 拿 `sk-...` project_key → 改一行 baseURL + 选择一种密钥传递方式
- 不持有真实 Key，没有加密 / KMS / 主密钥的运维负担
- trace 详情中 Authorization 与 project_key 都被 REDACT，最差也只是单条请求被截获

代价：失去"在 AILens360 一键撤销上游访问"的能力（要换上游 Key 必须去上游后台）；但 AILens360 自己的入口可以通过控制台「重置 project_key」即时失效。

#### 为什么把上游 URL 直接拼到路径里？

早期版本通过请求路径"自动识别 provider"，并把每种 provider 硬编码到一个官方 baseURL，结果**把 AILens360 锁死在了 3 个官方上游**：DeepSeek、Groq、Together、Moonshot、SiliconFlow、本地 vLLM / Ollama 全部进不来。

把**完整上游 URL 直接拼到代理路径**之后，"上游是谁"完全交还给客户端：

```
http://localhost:8080/https://api.deepseek.com/v1
http://localhost:8080/https://api.groq.com/openai/v1
http://localhost:8080/http://localhost:11434/v1            # Ollama
http://localhost:8080/https://api.anthropic.com
```

AILens360 侧不需要任何 per-project 上游配置，"Project 只是观测维度，不是路由约束"的精神保留。代价是客户端 baseURL 略长，但只配置一次，且 SDK 不感知。

### 7.3 为什么从一开始就用 Postgres + Redis + MinIO，而不是 SQLite？

虽然单机 SQLite 能"30 秒跑起来"，但：

- AILens360 处于 LLM 应用的关键路径，上线后立刻面临 trace 写入压力，SQLite 单写锁会卡住主流程
- project_key 解析必须支持多副本一致的失效广播，SQLite 路径无法做 Pub/Sub
- proxy 与 collector 之间需要持久化队列削峰，Redis Stream 是天然之选；SQLite 路径要么自己造 outbox 表，要么放弃异步
- 大 body 直接进 SQLite 会让数据库膨胀到不可备份，分库分表又把简单事情复杂化；S3 / MinIO 才是 body 该去的地方
- "等用户多了再迁移"意味着仓储 / 缓存 / 实时计数 / IPC / body 存储五处实现都要重做，业务代码也会被迫迁移
- 项目尚未上线就直接选分布式就绪基线，避免后续返工

代价是初次部署多三个依赖（一份 `docker compose -f docker-compose.deps.yml up -d` 起 Postgres + Redis + MinIO 就够），换得无状态可扩、Key 解析一致性、生产可用的实时指标、proxy 不被慢路径拖累。

### 7.4 为什么不强制 OpenTelemetry？

- OTEL 学习曲线高，对独立开发者不友好
- 但**对外可导出**：trace 可以以 OTLP 格式导出到 Jaeger / Tempo（v0.4+）
- 兼容而不绑定

## 八、性能与扩展

AILens360 处于 LLM 应用的**关键路径**——每一次调用都要经过它。核心原则：

- **代理路径独占进程**：proxy 进程不写 PG、不跑 pricing 计算、不跑 tokenizer。任何统计 / DB 代码挂掉都不会牵连代理
- **写入路径异步 + 队列削峰**：proxy XADD 到 Redis Stream → collector 消费；上游慢、PG 慢都不阻塞代理
- **三层缓存解决 project_key 热点**：L1 进程 LRU → L2 Redis → L3 Postgres，Pub/Sub 广播失效
- **正文外置**：MinIO/S3 承载 body 字节，PG `traces` 表瘦身，备份恢复都更轻
- **PG 月度分区**：单分区单调可控；超龄分区按 `RETENTION_MONTHS` 直接 DROP，不影响热分区

**水平扩缩容方案**：

| 维度 | 怎么扩 |
|---|---|
| 代理流量 | proxy 副本多开（无状态对等），前置 LB |
| 控制台 / API 请求 | api 副本多开（无状态对等）|
| Stream 消费速率 | collector 副本多开 —— 同一 consumer group 自动分片，XAUTOCLAIM 救场死副本 |
| Stream 堆积 | 升级 Redis 单机内存 / 切 Cluster |
| body 存储 | MinIO 分布式部署 / 切 AWS S3 |
| PG 写入 | 单实例足够时不必动；后续可上 Citus / 切 ClickHouse 明细 |

**性能目标（v1.0 验收）**：单 proxy 副本 ≥ 5000 QPS、并发流连接 10K+、代理透传开销 P99 < 5ms、集群线性扩展到 100K+ QPS。

## 九、未来扩展点

- **Replay**：选中历史 trace 一键重放（body 在 MinIO 易取，路由 v0.2+ 落地）
- **Project 级软配额 / 限流**：QPS / 日 token / 月成本上限（v0.3）
- **告警规则**：成本超阈值、错误率突增 → Webhook（v0.3）
- **预聚合指标表**：避免大时间窗口扫主表
- **OpenTelemetry 接收端点**：OTLP HTTP/gRPC
- **ClickHouse 明细存储**：取代 PG `traces` 分区方案，进一步压缩 + 加速分析查询
- **Plugin 系统**：Lua / JS 脚本做请求改写、prompt 增强
- **多用户 + RBAC**：Owner / Admin / Viewer + 控制台 API Key（v0.3）
