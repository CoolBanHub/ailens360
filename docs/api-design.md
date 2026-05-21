# API 与代理协议设计

## 一、代理协议设计

### 1.1 核心理念：URL 内嵌上游地址 + 项目密钥 + Authorization 透传

AILens360 的代理设计核心是 **"把完整上游 URL 直接拼到代理路径里，再从 Header / Path / Query 解析项目身份"**：

> 控制台创建 Project 时系统颁发一个 `project_key`（格式 `sk-` + 64 位 base62 字符）。应用层把全局唯一的 `proxy_prefix`（形如 `http://proxy-host:8080`，**proxy 进程的 origin**）后**直接拼上要访问的上游完整 URL（含 scheme）**作为 baseURL 使用，同时通过 `X-AILens-Project-Key`、`/<sk-...>/` 路径前缀或 `?sk=<sk-...>` 标识项目归属。**原本的 Authorization / x-api-key / x-goog-api-key 直接透传到真实上游**，AILens360 不持有真实 Key、也不预设上游地址。

代理 URL 格式：

```
http://{proxy-host}/{完整上游 URL，包含 scheme}
                   ↑
                   └── e.g. https://api.openai.com/v1/chat/completions

项目密钥（三选一；优先级 Header > Path > Query）：
Header: X-AILens-Project-Key: sk-...
Path:   http://{proxy-host}/sk-.../{完整上游 URL，包含 scheme}
Query:  http://{proxy-host}/{完整上游 URL，包含 scheme}?sk=sk-...
```

举例：

```
http://localhost:8080/https://api.openai.com/v1/chat/completions
http://localhost:8080/sk-demo.../https://api.openai.com/v1/chat/completions
http://localhost:8080/https://api.openai.com/v1/chat/completions?sk=sk-demo...
http://localhost:8080/https://api.anthropic.com/v1/messages
http://localhost:8080/https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-pro:generateContent
http://localhost:8080/https://api.deepseek.com/v1/chat/completions
http://localhost:8080/http://localhost:11434/v1/chat/completions   # Ollama / 本地 vLLM
```

应用层用法（以 OpenAI Python SDK 为例）：

```python
# Before
client = OpenAI(
    api_key="sk-real-openai-key",
    base_url="https://api.openai.com/v1",
)

# After：base_url 在上游 URL 前拼 proxy origin（http://localhost:8080），通过 default_headers 注入项目密钥
client = OpenAI(
    api_key="sk-real-openai-key",            # 仍然是你真实的上游 Key，直接透传
    base_url="http://localhost:8080/https://api.openai.com/v1",
    default_headers={"X-AILens-Project-Key": "sk-..."},
)
# SDK 会在 base_url 末尾自动追加 /chat/completions，
# 代理实际收到的请求是：
#   POST /https://api.openai.com/v1/chat/completions
#   X-AILens-Project-Key: sk-...
```

任何能被客户端写出完整 URL 的上游都能直接接入：DeepSeek、Groq、Together、Moonshot、本地 vLLM / Ollama、Anthropic、Gemini 官方端点等等，不需要在 AILens360 侧做任何 per-project 上游配置。

### 1.2 SSE 解析器：按上游 Host 内部选择

AILens360 对外**不暴露** provider 概念——trace 不带厂商标签、控制台也不按厂商筛选。代理内部需要的只是"用哪种 SSE 解析器"，按内嵌上游 URL 的 host 在 `internal/proxy/stream/` 包里选择：

| Host 命中规则 | 解析器 | 用途 |
|---|---|---|
| host 含 `anthropic` | `anthropic_parser` | Anthropic 风格 SSE 解析（`message_start` / `content_block_delta` / ...） |
| host 含 `googleapis` 或 `generativelanguage` | `gemini_parser` | Gemini 风格 SSE 解析 |
| 其他（`api.openai.com` / `api.deepseek.com` / `api.groq.com` / `localhost` / 任意自建） | `openai_parser`（默认） | OpenAI Chat Completions 兼容解析，业界事实标准 |

> 因为 OpenAI Chat Completions 已经是第三方上游的事实标准，所以"不匹配 anthropic / gemini host 一律按 openai 解析"足以覆盖绝大多数场景（DeepSeek、Groq、Together、Moonshot、本地 vLLM 都直接走 OpenAI 解析器）。

### 1.3 路由与转发逻辑

代理（proxy 进程）收到请求后的处理流程：

```
1. 解析 URL：r.URL.Path = /{完整上游 URL}
   - 因为上游 URL 含 "://"，chi 路由的 {var} 捕获会归一化 / 折叠 "//"，
     所以代理使用 /* 通配挂载（更具体的 /healthz 先匹配），
     再手动解析 r.URL.Path，剥掉前导 "/" 后校验 http:// 或 https:// 前缀
   - 实现见 internal/proxy/handler.go 中的 parseProxyPath
2. 从 Header / Path / Query 读取 project_key，解析 Project（带本地缓存）
   - Header：`X-AILens-Project-Key: sk-...`
   - Path：`/sk-.../<scheme>://<upstream>`（只接受 `sk-` 前缀，避免误识别普通路径）
   - Query：`?sk=sk-...`；作为项目密钥使用时会在转发上游前剥离
   - 缺失密钥：返回 401 missing_project_key
   - 命中：拿到 project_id 用于落库归因
   - 未命中：返回 404 project_not_found
3. 按 upstream.Host 在 stream 包内部选择对应的 SSE 解析器
   （openai / anthropic / gemini），仅影响流解析路径
4. 改写请求：
   - URL：直接使用 URL 中内嵌的完整上游 URL（scheme + host + path + query）
   - 透传客户端 Authorization / x-api-key / x-goog-api-key 到上游
   - 删除 AILens360 内部 Header（X-AILens-*，含 X-AILens-Project-Key）
5. 读完请求 body 后 → 起 goroutine 上传到 MinIO（请求 body）
6. 转发到上游，响应流式回写给客户端的同时：
   - 边写客户端边写 MinIO multipart uploader（响应 body）
   - 边写客户端边喂给 SSE 解析器（拿 token / first_token / finish_reason）
7. 关闭 multipart uploader 等 MinIO 完成；等步骤 5 的请求 body 上传完成
8. 构造 stream.Event（含 body keys + sizes、解析出的 token/usage、Timeline）
   - 存储前 REDACT Authorization / Cookie / x-api-key / x-goog-api-key / X-AILens-Project-Key
   - JSON marshal → XADD 到 Redis Stream "ailens360:traces"
9. collector 进程异步从 Stream 消费，写 PG `traces` 分区表 + Redis 实时指标
   - trace.request_path 记录上游绝对 URL，便于回放与人肉排查
```

### 1.4 Project 的设计要点

#### project_key 命名规则
- 默认：`sk-` + 64 位随机字符串，随机部分字符集 `0-9A-Za-z`（base62），约 381 bits 熵
- 全局唯一；创建时若与既有项目冲突，最多重试 10 次
- Project 删除后 project_key 不复用
- 控制台支持「重置 project_key」：旧密钥立即返回 401，需要把所有客户端同步改为新值
- 64 位 base62 随机部分在密码学上无法被穷举枚举；若用 Path / Query 模式，反向代理访问日志会更容易记录密钥，生产环境更推荐 Header 模式

#### Project 字段
- `id`：内部主键（ULID）
- `project_key`：`sk-` + 64 位 base62，作为 Header / Path / Query 中的项目凭据，**视同密钥**
- `name`：人类可读名字，仅用于控制台展示
- `created_at` / `updated_at`：时间戳

控制台 API 返回 Project 时会附带 `proxy_prefix = scheme://proxy-host[:port]`（proxy 进程的 origin，所有项目共用），以及一个 `example` 对象（包含 OpenAI / Anthropic / Gemini 三个常用上游的开箱即用 baseURL，并提供 `path_key` / `query_key` 两组带密钥的 URL），方便前端直接复制使用。当 `AILENS360_PUBLIC_URL` / `AILENS360_PROXY_PUBLIC_URL` 未配置时，api 进程会从请求头派生并自动替换为 proxy 监听端口（本地开发零配置就能工作）。

#### 撤销与失效
- 控制台删除 Project，所有携带该 `project_key` 的请求立刻返回 401 / 404
- 因为 AILens360 不持有上游 Key，撤销上游访问的最终手段是**在上游平台轮换真实 Key**

### 1.5 元数据透传

应用还可以通过自定义 Header 补充上下文（用于更细粒度的聚合）：

| Header | 作用 |
|---|---|
| `X-AILens-Project-Key` | 可选。项目密钥，标识请求归属哪个 Project；也可用 Path / Query 模式 |
| `X-AILens-User` | 终端用户 ID |
| `X-AILens-Session` | 会话 ID（用于聚合一段对话） |
| `X-AILens-Tag` | 自定义标签（逗号分隔） |
| `X-AILens-Trace-Id` / `X-AILens-Trace-Name` | 逻辑 trace 归组 |

这些 Header **不会**被透传到上游 LLM，只用于 AILens360 内部记录。

### 1.6 安全约束

- 生产环境强制 **HTTPS**，避免 Authorization 与 project_key 在传输层泄露
- `project_key` 默认 `sk-` + 64 位 base62（约 381 bits 熵），密码学上不可穷举；视同密钥保管，泄露后通过控制台「重置 project_key」即时轮换
- Header 模式最不容易进入访问日志；Path / Query 模式适合不方便设置自定义 Header 的客户端，部署时需要注意网关日志脱敏
- AILens360 **永远不存储**客户端透传的上游 Key；trace 详情 / trace 列表 API 都会 REDACT Authorization / Cookie / x-api-key / x-goog-api-key / X-AILens-Project-Key
- Header 中的上游密钥会强制 REDACT；prompt / completion 内容自动脱敏仍在规划中，当前不会自动屏蔽手机号 / 邮箱等正文敏感信息

## 二、内部 REST API

### 2.1 通用约定

- **Base Path**：`/api`
- **认证**：先通过 `POST /api/auth/login` 用用户名密码换取 JWT，后续接口都带 `Authorization: Bearer <jwt>`
- **响应格式**：JSON，统一外壳

```json
{
  "code": 0,
  "message": "ok",
  "data": { ... }
}
```

错误响应：

```json
{
  "code": 40001,
  "message": "invalid request",
  "data": null
}
```

### 2.2 认证

```
POST /api/auth/login
GET  /api/auth/me
```

`POST /api/auth/login`（**公开**）：

```json
// 请求
{ "username": "admin", "password": "..." }

// 响应
{
  "code": 0,
  "data": {
    "token": "<jwt>",
    "expires_at": 1779180625
  }
}
```

- 用户名 / 密码来自配置 `auth.username` / `auth.password`（默认 `admin` / `admin`，生产前**必须**改掉，可走 `AILENS360_AUTH_USERNAME` / `AILENS360_AUTH_PASSWORD` 环境变量）
- JWT 签名密钥由 `auth.jwt_secret` / `AILENS360_JWT_SECRET` 控制；api 进程中留空会启动失败，避免多副本或重启后会话行为不一致
- 默认 token TTL = 168h（7 天），由 `auth.token_ttl` 配置

`GET /api/auth/me`（**需 JWT**）：返回当前登录信息，给前端做"我是谁"展示。

### 2.3 Trace 相关

#### 列出 Trace

```
GET /api/traces
```

Query 参数：
- `project_id`：项目过滤
- `trace_id`：逻辑 trace 过滤（展开某个 trace group 下的 spans）
- `user_id`：用户过滤
- `session_id`：会话过滤
- `model`：模型过滤
- `status`：success / error
- `start_time` / `end_time`：时间范围（unix ms）
- `limit` / `offset`：分页（默认 50）

返回：

```json
{
  "code": 0,
  "data": {
    "total": 12450,
    "items": [
      {
        "ID": "tr_a1b2c3d4",
        "TraceID": "run_xyz",
        "TraceName": "customer_support_agent",
        "ProjectID": "prj_xxx",
        "UserID": "user_001",
        "SessionID": "sess_xxx",
        "Tags": "prod,chatbot",
        "Model": "gpt-4o-mini",
        "IsStream": true,
        "Status": "success",
        "StatusCode": 200,
        "RequestPath": "https://api.openai.com/v1/chat/completions",
        "RequestHeaders": "{...JSON, Authorization/Cookie/x-api-key 已 REDACT...}",
        "ResponseHeaders": "{...JSON...}",
        "RequestBodyKey": "prj_xxx/202605/tr_a1b2c3d4/request.json",
        "ResponseBodyKey": "prj_xxx/202605/tr_a1b2c3d4/response.bin",
        "RequestBodySize": 234,
        "ResponseBodySize": 1567,
        "Timeline": "[{\"event\":\"request_received\",\"ts\":1731628800000}]",
        "InputTokens": 234,
        "OutputTokens": 567,
        "TotalTokens": 801,
        "ReasoningTokens": 0,
        "CachedInputTokens": 0,
        "CacheCreationInputTokens": 0,
        "TokensEstimated": false,
        "CostUSD": 0.00123,
        "LatencyMs": 1234,
        "TTFTMs": 345,
        "TTFBMs": 188,
        "GenDurationMs": 889,
        "TPS": 637.8,
        "ChunkCount": 567,
        "BytesStreamed": 18342,
        "FinishReason": "stop",
        "StreamStatus": "completed",
        "CreatedAt": "2026-05-15T10:00:00Z"
      }
    ]
  }
}
```

后端当前直接序列化 `repo.Trace`，所以字段名保持 Go 结构体的 PascalCase。支持的 Query 过滤为：`project_id` / `trace_id` / `user_id` / `session_id` / `model` / `status` / `start_time` / `end_time` / `limit` / `offset`。`limit <= 0` 时默认取 50 条，生产用法建议显式指定 `limit + offset` 翻页。

字段速查：

| 字段 | 含义 |
|---|---|
| `TTFTMs` | **首 token 延迟（核心 KPI）**：从客户端请求进入到第一个 token 输出 |
| `TTFBMs` | 上游 HTTP 首字节延迟 |
| `GenDurationMs` | 纯生成时长（首 token → 末 token） |
| `TPS` | 输出吞吐 tokens/sec |
| `ChunkCount` | SSE chunk 总数 |
| `TokensEstimated` | true 表示 output_tokens 是本地 tokenizer 估算（非上游返回） |
| `StreamStatus` | completed / aborted / errored / stalled |
| `RequestPath` | 本次请求转发到上游的**绝对 URL**（含 scheme + host + path + query） |

> 非流式请求的 `TTFTMs` 为 `null`，避免把完整响应耗时误统计为首 token 延迟。

#### 单个 Trace 详情

```
GET /api/traces/:id
```

返回 span 的元数据（**不含** body 字节 —— body 走单独端点）：

```json
{
  "code": 0,
  "data": {
    "ID": "tr_a1b2c3d4",
    "TraceID": "run_xyz",
    "ProjectID": "prj_xxx",
    "Model": "gpt-4o-mini",
    "IsStream": true,
    "Status": "success",
    "StatusCode": 200,
    "RequestPath": "https://api.openai.com/v1/chat/completions",
    "RequestHeaders": "{...JSON, Authorization/Cookie/x-api-key 已 REDACT...}",
    "ResponseHeaders": "{...JSON...}",
    "RequestBodyKey":  "prj_xxx/202605/tr_a1b2c3d4/request.json",
    "ResponseBodyKey": "prj_xxx/202605/tr_a1b2c3d4/response.bin",
    "RequestBodySize":  234,
    "ResponseBodySize": 1567,
    "Timeline": "[{\"event\":\"request_received\",\"ts\":1731628800000}, ...]",
    "InputTokens": 234, "OutputTokens": 567, "TotalTokens": 801,
    "LatencyMs": 1234, "TTFTMs": 345, "TTFBMs": 188,
    "TPS": 637.8,
    "ChunkCount": 567,
    "FinishReason": "stop",
    "StreamStatus": "completed",
    "CreatedAt": "2026-05-15T10:00:00Z"
  }
}
```

#### 拉取 Trace Body（请求 / 响应正文）

```
GET /api/traces/:id/body?part=request|response
```

正文存在对象存储（MinIO/S3），api 进程根据 `AILENS360_BODY_STORE_PRESIGN_REDIRECT` 决定怎么交付：

- **默认（false）**：api 从 MinIO 拉字节流式转发给浏览器
  - 响应头 `Content-Encoding: gzip`（若 body 上传时开了 gzip）；浏览器或 `curl --compressed` 自动解码
  - MinIO 完全可以待在内网
- **PRESIGN_REDIRECT=true**：返回 `302 Found`，`Location` 为短期 presigned MinIO URL
  - 浏览器直接拉 MinIO，api 不吃带宽
  - 要求 MinIO 对浏览器可达 + 配 CORS

错误：
- `404 body not stored for this trace`：上传当时失败 / 跳过，trace 元数据存在但 body_key 为空
- `404 trace not found`：trace_id 不存在
- `400 part must be 'request' or 'response'`

> 老版本中详情接口直接内嵌 `request_body` / `response_body` / `stream_chunks` —— 这套已彻底废弃，正文一律走对象存储。

#### 列出 Trace Group（按逻辑 trace 聚合，Langfuse 风格）

```
GET /api/trace_groups
```

每行对应一个 `trace_id`，把同一次 Agent 运行的多次模型调用聚合成一行：`span_count` / `input_tokens` / `output_tokens` / `total_tokens` / `cost_usd` 求和；`latency_ms` = 组内 `max(created_at) − min(created_at)`；`status` 取组内最坏态（`error > aborted > success`）。

支持过滤：`project_id` / `user_id` / `session_id` / `trace_name`（精确匹配）/ `model`（命中任一 span 即匹配）/ `status` / `start_time` / `end_time` / `limit` / `offset`。

> 同一组内的各 span 详情仍走 `GET /api/traces?trace_id=<id>` 展开。

#### 重放 Trace（规划中，尚未实现）

```
POST /api/traces/:id/replay      # v0.2+
```

`request_path` 字段已经把上游绝对 URL 写入了 trace（迁移 0003），重放接口的实现条件已经满足，但路由本身尚未挂在 `internal/api/router.go` 中。落地时调用方需要在 body 中显式提供一个可用的上游 `authorization`（原 Header 已 REDACT，不可恢复）。

### 2.4 指标统计

#### 用量统计

```
GET /api/metrics/usage
```

参数：
- `dimension`：聚合维度（`model` / `project` / `day` / `hour`）
- `start_time` / `end_time`
- `project_id`（可选过滤）

返回：

```json
{
  "code": 0,
  "data": {
    "dimension": "model",
    "items": [
      {
        "Key": "gpt-4o-mini",
        "Calls": 12450,
        "InputTokens": 1245000,
        "OutputTokens": 567000,
        "TotalTokens": 1812000,
        "ReasoningTokens": 0,
        "CachedInputTokens": 0,
        "CacheCreationInputTokens": 0,
        "CostUSD": 12.34,
        "AvgLatencyMs": 1234,
        "ErrorRate": 0.0023
      }
    ]
  }
}
```

#### 实时指标

```
GET /api/metrics/live?project_id=prj_xxx
```

从 Redis 实时计数器读取近窗口数据（默认 60s），用于控制台轮询刷新；这是近实时观测值，不作为账单口径。

```json
{
  "code": 0,
  "data": {
    "project_id": "prj_xxx",
    "qps": 1.2,
    "tokens_per_sec": 534.5,
    "cost_usd_per_s": 0.00042
  }
}
```

#### Trace Facets

```
GET /api/trace_facets?project_id=prj_xxx
```

返回指定项目下出现过的模型列表和是否存在任何 trace，用于前端筛选项和空状态判断。

```json
{
  "code": 0,
  "data": {
    "models": ["gpt-4o-mini", "claude-3-5-sonnet"],
    "has_data": true
  }
}
```

### 2.5 Project

Project 是唯一的一级实体，承担"归因维度 + 代理入口"两件事。AILens360 不再像早期版本那样区分 upstream / short_link / proxy_key——所有上游 URL 和真实 Authorization 由客户端在 baseURL / Header 中直接提供。

```
POST   /api/projects                          # 创建 Project，返回 project_key、proxy_prefix 与 example
GET    /api/projects                          # 列表
GET    /api/projects/:id                      # 详情
PUT    /api/projects/:id                      # 更新（目前仅 name）
POST   /api/projects/:id/reset_project_key    # 重置 project_key（旧密钥立即失效，返回新 Project）
DELETE /api/projects/:id                      # 删除（删除后 project_key 立刻失效）
```

请求 / 响应示例（api 在 :8081）：

```bash
curl -X POST http://localhost:8081/api/projects \
     -H 'Authorization: Bearer $ADMIN_TOKEN' \
     -H 'Content-Type: application/json' \
     -d '{"name":"my-app"}'
```

```json
{
  "code": 0,
  "data": {
    "id": "prj_01HV...",
    "project_key": "sk-demo...",
    "name": "my-app",
    "proxy_prefix": "http://localhost:8080",
    "example": {
      "openai":    "http://localhost:8080/https://api.openai.com/v1",
      "anthropic": "http://localhost:8080/https://api.anthropic.com",
      "gemini":    "http://localhost:8080/https://generativelanguage.googleapis.com/v1beta",
      "path_key": {
        "openai":    "http://localhost:8080/sk-demo.../https://api.openai.com/v1",
        "anthropic": "http://localhost:8080/sk-demo.../https://api.anthropic.com",
        "gemini":    "http://localhost:8080/sk-demo.../https://generativelanguage.googleapis.com/v1beta"
      },
      "query_key": {
        "openai":    "http://localhost:8080/https://api.openai.com/v1?sk=sk-demo...",
        "anthropic": "http://localhost:8080/https://api.anthropic.com?sk=sk-demo...",
        "gemini":    "http://localhost:8080/https://generativelanguage.googleapis.com/v1beta?sk=sk-demo..."
      }
    },
    "created_at": 1778575825,
    "updated_at": 1778575825
  }
}
```

> `proxy_prefix` 是 proxy 进程的 origin（所有项目共用，不是最终 baseURL）：使用时在它后面继续拼接完整的上游 URL（含 scheme），客户端 SDK 通常会在此基础上再追加自己的相对路径。`example` 字段给出了三个最常见上游的开箱即用形态，前端可以直接复制。项目身份可通过 `X-AILens-Project-Key: <project_key>`、`/<project_key>/` 路径前缀或 `?sk=<project_key>` 识别。

接入应用（以 OpenAI 官方端点为例）：

```bash
curl -X POST 'http://localhost:8080/https://api.openai.com/v1/chat/completions' \
     -H 'Authorization: Bearer sk-real-openai-key' \
     -H 'X-AILens-Project-Key: sk-demo...' \
     -H 'Content-Type: application/json' \
     -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
```

等价路径模式：

```bash
curl -X POST 'http://localhost:8080/sk-demo.../https://api.openai.com/v1/chat/completions' \
     -H 'Authorization: Bearer sk-real-openai-key' \
     -H 'Content-Type: application/json' \
     -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'
```

换成 DeepSeek / Groq / 本地 vLLM 只需要把上游 URL 段换掉，其它都不变。

### 2.6 实时流（规划中，尚未实现）

`WS /api/stream` 计划用于 Web Console 的"实时调用墙"，连接后推送 `trace.created` 事件，支持按 `project_id` / `model` 过滤。当前控制台靠定时轮询 `GET /api/traces` 拿最新数据，规划 v0.2 引入 WebSocket。

## 三、SDK 设计（v0.4+）

虽然主推无需 SDK 的代理接入，但提供可选 SDK 用于：
- 显式打 tag / metadata
- 关联应用层 trace
- 手动补充信息（如评分）

```go
// Go SDK 示例
ail := ailens360.New("ail-ak-xxx")  // 控制台 API Key，仅用于补充元数据

ctx := ail.StartSession(context.Background(),
    ailens360.WithUser("user_001"),
    ailens360.WithTags("chatbot", "prod"),
)

// 后续 OpenAI SDK 自动通过 ctx 携带元数据
resp, _ := openaiClient.Chat.Completions.New(ctx, req)

// 可选：补充评分
ail.Annotate(traceID, ailens360.Score(0.95), ailens360.Feedback("good"))
```

## 四、OpenTelemetry 兼容

### 4.1 接收 OTEL 数据（规划中）

AILens360 提供标准 OTLP 接收端点：

```
POST /v1/traces  (HTTP/JSON)
gRPC :4317
```

允许已经用 OTEL 的应用直接把数据发给 AILens360（不走代理也能用）。

### 4.2 导出到外部 OTEL 后端（规划中）

控制台配置导出器，把 AILens360 的 trace 转发到：
- Jaeger
- Tempo
- Datadog
- 任何 OTLP 兼容后端

## 五、未来扩展

- **GraphQL API**：方便前端按需取字段
- **Webhook**：trace 满足条件时回调（异常告警、特殊事件）
- **批量 API**：批量导出 trace 用于离线分析
