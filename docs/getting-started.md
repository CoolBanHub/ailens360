# 快速开始

> 目标：5 分钟内本地跑起来，创建第一个 Project，发起第一条带观测的 LLM 调用。

## 环境要求

- Go 1.26+（**无需 CGO**；当前 `go.mod` 声明 `go 1.26.1`）
- Postgres 14+、Redis 6+、MinIO（或任意 S3 兼容对象存储）—— 都是必备依赖；本地最简方式：`docker compose -f docker-compose.deps.yml up -d`
- Node.js 20+ 与 pnpm（仅当需要本地跑前端控制台）
- 任意 LLM 上游 API Key（OpenAI / Anthropic / Gemini / DeepSeek / Grok / 本地 vLLM / Ollama 均可）

## 一、构建与启动

AILens360 由**三个进程**共同对外提供服务：

| 进程 | 端口 | 职责 |
|---|---|---|
| `ailens360 proxy` | `:8080` | LLM 流量入口（反向代理） |
| `ailens360 collector` | `:8082` (健康检查) | 消费 Redis Stream → 写 PG / 实时指标 / 自动分区 |
| `ailens360 api` | `:8081` | REST 控制台 + 静态 UI + body presign |

```bash
git clone https://github.com/CoolBanHub/ailens360.git
cd ailens360

# 构建（输出 ./bin/ailens360 —— 同一个二进制，靠子命令分进程）
make build

# 配置走 .env（项目根目录），首次拷贝模板后按需修改
cp .env.example .env
# 必填：AILENS360_JWT_SECRET（仅 api 进程需要）
# 用 openssl rand -hex 32 生成

# 启动依赖（Postgres + Redis + MinIO）
docker compose -f docker-compose.deps.yml up -d

# 同时起三个进程（前台）
make run
```

`make run` 把三个进程跑在同一个 shell 里，日志交错输出；Ctrl-C 一起停。需要单独起或观察某一个：

```bash
make run-collector  # 跑迁移、消费 Stream、分区维护
make run-proxy      # 反向代理 :8080
make run-api        # REST + UI :8081
```

> collector 启动时会自动检查并 apply `internal/storage/postgres/migrations/` 下的迁移；其他进程启动不动 schema。

三个进程的健康检查：

```bash
curl http://localhost:8080/healthz   # proxy
curl http://localhost:8081/healthz   # api
curl http://localhost:8082/healthz   # collector
```

## 二、登录控制台

默认凭据来自 `.env` 的 `AILENS360_AUTH_USERNAME` / `AILENS360_AUTH_PASSWORD`（默认 `admin` / `admin`）。生产部署前**务必**改掉。登录是 **api 进程**（`:8081`）的事：

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jq -r '.data.token')

echo "$TOKEN"
```

返回的 `token` 是 JWT，后续所有 `/api/*`（除 `/auth/login`）请求都需要带上 `Authorization: Bearer $TOKEN`。

> JWT 默认有效期 168h（7 天），可通过 `AILENS360_AUTH_TOKEN_TTL` 调整。

## 三、创建 Project

```bash
curl -X POST http://localhost:8081/api/projects \
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
    "project_key": "sk-demo...",
    "name": "hello-ailens",
    "proxy_prefix": "http://localhost:8080",
    "example": {
      "openai":    "http://localhost:8080/https://api.openai.com/v1",
      "anthropic": "http://localhost:8080/https://api.anthropic.com",
      "gemini":    "http://localhost:8080/https://generativelanguage.googleapis.com/v1beta",
      "path_key": {
        "openai": "http://localhost:8080/sk-demo.../https://api.openai.com/v1"
      },
      "query_key": {
        "openai": "http://localhost:8080/https://api.openai.com/v1?sk=sk-demo..."
      }
    },
    "created_at": 1778575825,
    "updated_at": 1778575825
  }
}
```

把 `example.openai` 当成应用 SDK 的 `base_url`，再把 `project_key` 放进 `X-AILens-Project-Key` 头即可。也可以直接使用 `example.path_key.openai`，把 `sk-...` 放在 URL 路径里；或使用 `example.query_key.openai`，把 `sk-...` 放在查询参数里。`proxy_prefix` 是 **proxy 进程的 origin**（自动识别请求 host 并替换为 proxy 端口；生产建议显式配 `AILENS360_PUBLIC_URL`）。

## 四、发起一次带观测的调用

代理 URL 形态：**`http://localhost:8080/{完整上游 URL}`**（注意 `/` 后直接拼 `http://` 或 `https://`，无 `/p/` 前缀）。项目密钥支持三种来源，优先级是 Header > Path > Query：

```text
Header: X-AILens-Project-Key: sk-xxx
Path:   http://localhost:8080/sk-xxx/https://api.openai.com/v1/chat/completions
Query:  http://localhost:8080/https://api.openai.com/v1/chat/completions?sk=sk-xxx
```

### curl 直发

```bash
curl -X POST 'http://localhost:8080/https://api.openai.com/v1/chat/completions' \
  -H 'Authorization: Bearer sk-real-openai-key' \
  -H 'X-AILens-Project-Key: sk-demo...' \
  -H 'Content-Type: application/json' \
  -d '{
        "model":"gpt-5.5",
        "stream": true,
        "messages":[{"role":"user","content":"hello"}]
      }'
```

> 请求中的 `Authorization` 是**真实的 OpenAI Key**，AILens360 仅在内存中透传给上游，**不会写入数据库**。`project_key` 用于识别项目归属，**视同密钥**；Header 模式落库前会被 REDACT，Query 模式中的 `sk` 会在转发上游前剥离。请求/响应正文上传到 MinIO，PG 只存对象 key。

### OpenAI Python SDK

```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-real-openai-key",
    base_url="http://localhost:8080/https://api.openai.com/v1",
    default_headers={"X-AILens-Project-Key": "sk-demo..."},
)

resp = client.chat.completions.create(
    model="gpt-5.5",
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
    base_url="http://localhost:8080/https://api.anthropic.com",
    default_headers={"X-AILens-Project-Key": "sk-demo..."},
)
```

### 接入第三方 / 自建 OpenAI 兼容上游

只换 baseURL 末尾的上游 URL（项目密钥传递方式照旧）：

```python
# DeepSeek
base_url="http://localhost:8080/https://api.deepseek.com/v1"
# Grok
base_url="http://localhost:8080/https://api.x.ai/v1"
# 本地 Ollama
base_url="http://localhost:8080/http://localhost:11434/v1"
```

## 五、附加元数据（可选）

应用可以通过自定义 Header 给 trace 打标，用于后续按用户 / 会话 / 业务标签下钻：

| Header | 作用 |
|---|---|
| `X-AILens-Project-Key` | 可选。项目密钥（创建 Project 时颁发）；也可以用路径前缀 `/<sk-...>/` 或 query `?sk=<sk-...>` |
| `X-AILens-User` | 终端用户 ID |
| `X-AILens-Session` | 会话 ID（同一会话内多次调用聚合） |
| `X-AILens-Tag` | 自定义标签，逗号分隔 |
| `X-AILens-Trace-Id` | 逻辑 trace ID（Langfuse 风格，一次 Agent 运行内多次模型调用共用同一 trace_id） |
| `X-AILens-Trace-Name` | 逻辑 trace 的人类名字 |

这些 Header **不会**转发到上游，仅用于落库归因。`X-AILens-Project-Key` 落库前会被 REDACT。

```bash
curl -X POST 'http://localhost:8080/https://api.openai.com/v1/chat/completions' \
  -H 'Authorization: Bearer sk-real-openai-key' \
  -H 'X-AILens-Project-Key: sk-demo...' \
  -H 'X-AILens-User: user_001' \
  -H 'X-AILens-Session: sess_abc' \
  -H 'X-AILens-Tag: prod,chatbot' \
  -H 'X-AILens-Trace-Id: run_xyz' \
  -H 'X-AILens-Trace-Name: customer_support_agent' \
  ...
```

## 六、查看 trace

### 通过 API（在 api 进程的 :8081）

```bash
# Span 列表
curl -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8081/api/traces?project_id=prj_01HV...&limit=20'

# 单条 span 详情（不含 body，body 走单独端点）
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8081/api/traces/tr_xxx

# 拉回 trace 的 request / response body（流式转发自 MinIO，gzip 自动协商）
curl -H "Authorization: Bearer $TOKEN" --compressed \
  'http://localhost:8081/api/traces/tr_xxx/body?part=request'

# 按逻辑 trace 聚合（一次 Agent 运行多步调用合一行）
curl -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8081/api/trace_groups?project_id=prj_01HV...&limit=20'

# 用量聚合（dimension: model | project | day | hour）
curl -H "Authorization: Bearer $TOKEN" \
  'http://localhost:8081/api/metrics/usage?dimension=model&start_time=...&end_time=...'
```

### 通过 Web Console

前端在 api 进程托管。docker compose 部署时直接访问 `http://localhost:8081/` 即可。

本地开发可单独启动 Vite dev server（HMR 更快）：

```bash
cd frontend
pnpm install
pnpm dev
```

打开 `http://localhost:5173`，用上面的 admin 账号登录。Vite dev 配置已经把 `/api` 反代到 api 进程的 `:8081`。

主要页面：

- `/` —— 项目列表
- `/project/:id/overview` —— 项目维度的概览大盘
- `/project/:id/setup` —— 复制 proxy_prefix / 示例 base_url
- `/project/:id/traces` —— Trace 列表与过滤
- `/project/:id/trace/:traceId` —— Trace 详情（请求 / 响应 / 时间线，body 按需从 MinIO 拉）
- `/project/:id/settings` —— 项目设置

## 七、下一步

- 不要把 `AILENS360_AUTH_PASSWORD` 保留为 `admin`
- 用 `AILENS360_JWT_SECRET` 固定 JWT 密钥（仅 api 进程需要），避免重启后会话失效
- 生产部署见 [`deployment.md`](./deployment.md)
- 想了解 Schema 字段含义见 [`architecture.md` §五 数据模型](./architecture.md#五数据模型)
- 想做开发贡献见 [`contributing.md`](./contributing.md)
