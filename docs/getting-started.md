# 快速开始

> 目标：5 分钟内本地跑起来，创建第一个 Project，发起第一条带观测的 LLM 调用。

## 环境要求

- Go 1.22+（**无需 CGO**）
- Postgres 14+ 与 Redis 6+（必备依赖；本地最简方式：`docker compose -f docker-compose.deps.yml up -d`，只会起 PG 与 Redis）
- Node.js 20+ 与 pnpm（仅当需要本地跑前端控制台）
- 任意 LLM 上游 API Key（OpenAI / Anthropic / Gemini / DeepSeek / Groq / 本地 vLLM / Ollama 均可）

## 一、构建与启动后端

```bash
git clone https://github.com/CoolBanHub/ailens360.git
cd ailens360

# 构建（输出 ./bin/ailens360）
make build

# 配置走 .env（项目根目录），首次拷贝模板后按需修改
cp .env.example .env
# 必填：AILENS360_JWT_SECRET、AILENS360_DB_DSN、AILENS360_REDIS_ADDR
# AILENS360_JWT_SECRET 用 openssl rand -hex 32 生成

./bin/ailens360 server
```

> 启动时会自动检查并 apply `internal/storage/postgres/migrations/` 下的迁移。

启动后默认监听 `:8080`，同一端口同时承载：

- `POST /p/{完整上游 URL}` + `X-AILens-Project-Key` 头 —— 代理流量入口
- `GET /api/...` —— 控制台 API（受 JWT 保护）
- `GET /healthz` —— 健康检查（返回 `ok`）
- `GET /version` —— 版本号

> 如果只想验证服务是否起来：`curl http://localhost:8080/healthz`

## 二、登录控制台

默认凭据来自 `.env` 的 `AILENS360_AUTH_USERNAME` / `AILENS360_AUTH_PASSWORD`（默认 `admin` / `admin`）。生产部署前**务必**改掉。

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jq -r '.data.token')

echo "$TOKEN"
```

返回的 `token` 是 JWT，后续所有 `/api/*`（除 `/auth/login`）请求都需要带上 `Authorization: Bearer $TOKEN`。

> JWT 默认有效期 168h（7 天），可通过 `AILENS360_AUTH_TOKEN_TTL` 调整。

## 三、创建 Project

```bash
curl -X POST http://localhost:8080/api/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"hello-ailens"}'
```

返回示例：

```json
{
  "code": 0,
  "data": {
    "id": "prj_01HV...",
    "project_key": "kJ8s...64-char-base62...",
    "name": "hello-ailens",
    "proxy_prefix": "http://localhost:8080/p",
    "example": {
      "openai":    "http://localhost:8080/p/https://api.openai.com/v1",
      "anthropic": "http://localhost:8080/p/https://api.anthropic.com",
      "gemini":    "http://localhost:8080/p/https://generativelanguage.googleapis.com/v1beta"
    },
    "created_at": 1778575825,
    "updated_at": 1778575825
  }
}
```

把 `example.openai` 当成应用 SDK 的 `base_url`，再把 `project_key` 放进 `X-AILens-Project-Key` 头即可。

## 四、发起一次带观测的调用

### curl 直发

```bash
curl -X POST 'http://localhost:8080/p/https://api.openai.com/v1/chat/completions' \
  -H 'Authorization: Bearer sk-real-openai-key' \
  -H 'X-AILens-Project-Key: kJ8s...64-char-base62...' \
  -H 'Content-Type: application/json' \
  -d '{
        "model":"gpt-4o-mini",
        "stream": true,
        "messages":[{"role":"user","content":"hello"}]
      }'
```

> 请求中的 `Authorization` 是**真实的 OpenAI Key**，AILens360 仅在内存中透传给上游，**不会写入数据库**。`X-AILens-Project-Key` 用于识别项目归属，**视同密钥**，落库前会被 REDACT。

### OpenAI Python SDK

```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-real-openai-key",
    base_url="http://localhost:8080/p/https://api.openai.com/v1",
    default_headers={"X-AILens-Project-Key": "kJ8s...64-char-base62..."},
)

resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "hello"}],
    stream=True,
)
for chunk in resp:
    print(chunk.choices[0].delta.content or "", end="", flush=True)
```

### Anthropic Python SDK

```python
import anthropic

client = anthropic.Anthropic(
    api_key="sk-ant-real-key",
    base_url="http://localhost:8080/p/https://api.anthropic.com",
    default_headers={"X-AILens-Project-Key": "kJ8s...64-char-base62..."},
)
```

### 接入第三方 / 自建 OpenAI 兼容上游

只换 baseURL 末尾的上游 URL（`X-AILens-Project-Key` 头照旧）：

```python
# DeepSeek
base_url="http://localhost:8080/p/https://api.deepseek.com/v1"
# Groq
base_url="http://localhost:8080/p/https://api.groq.com/openai/v1"
# 本地 Ollama
base_url="http://localhost:8080/p/http://localhost:11434/v1"
```

## 五、附加元数据（可选）

应用可以通过自定义 Header 给 trace 打标，用于后续按用户 / 会话 / 业务标签下钻：

| Header | 作用 |
|---|---|
| `X-AILens-Project-Key` | **必填**。项目密钥（创建 Project 时颁发） |
| `X-AILens-User` | 终端用户 ID |
| `X-AILens-Session` | 会话 ID（同一会话内多次调用聚合） |
| `X-AILens-Tag` | 自定义标签，逗号分隔 |
| `X-AILens-Trace-Id` | 逻辑 trace ID（Langfuse 风格，一次 Agent 运行内多次模型调用共用同一 trace_id） |
| `X-AILens-Trace-Name` | 逻辑 trace 的人类名字 |

这些 Header **不会**转发到上游，仅用于落库归因。`X-AILens-Project-Key` 落库前会被 REDACT。

```bash
curl -X POST 'http://localhost:8080/p/https://api.openai.com/v1/chat/completions' \
  -H 'Authorization: Bearer sk-real-openai-key' \
  -H 'X-AILens-Project-Key: kJ8s...64-char-base62...' \
  -H 'X-AILens-User: user_001' \
  -H 'X-AILens-Session: sess_abc' \
  -H 'X-AILens-Tag: prod,chatbot' \
  -H 'X-AILens-Trace-Id: run_xyz' \
  -H 'X-AILens-Trace-Name: customer_support_agent' \
  ...
```

## 六、查看 trace

### 通过 API

```bash
# Span 列表
curl -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/traces?project_id=prj_01HV...&limit=20'

# 单条 span 详情
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/traces/tr_xxx

# 按逻辑 trace 聚合（一次 Agent 运行多步调用合一行）
curl -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/trace_groups?project_id=prj_01HV...&limit=20'

# 用量聚合（dimension: model | project | day | hour）
curl -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8080/api/metrics/usage?dimension=model&start_time=...&end_time=...'
```

### 通过 Web Console

前端是独立的 Vite 项目，**不内嵌**到 Go 二进制：

```bash
cd frontend
pnpm install
pnpm dev
```

打开 `http://localhost:5173`，用上面的 admin 账号登录。如果跨域被拦截，把 `http://localhost:5173` 加入后端 `AILENS360_API_CORS_ORIGINS`（逗号分隔）。

主要页面：

- `/` —— 项目列表
- `/project/:id/overview` —— 项目维度的概览大盘
- `/project/:id/setup` —— 复制 proxy_prefix / 示例 base_url
- `/project/:id/traces` —— Trace 列表与过滤
- `/project/:id/trace/:traceId` —— Trace 详情（请求 / 响应 / 流式 chunk / 时间线）
- `/project/:id/settings` —— 项目设置

## 七、下一步

- 不要把 `AILENS360_AUTH_PASSWORD` 保留为 `admin`
- 用 `AILENS360_JWT_SECRET` 固定 JWT 密钥，避免重启后会话失效
- 生产部署见 [`deployment.md`](./deployment.md)
- 想了解 Schema 字段含义见 [`architecture.md` §五 数据模型](./architecture.md#五数据模型)
- 想做开发贡献见 [`contributing.md`](./contributing.md)
