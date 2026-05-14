# 贡献指南

欢迎参与 AILens360。项目处于早期阶段，每个 Issue / PR / Discussion 都会被认真看待。

## 一、开发环境

- Go **1.22+**（**无 CGO**）
- Postgres **14+** 与 Redis **6+**（必备依赖；本地直接 `docker compose -f docker-compose.deps.yml up -d` 起一份）
- Node.js **20+** 与 pnpm（仅当涉及 `frontend/` 改动）
- `make` / `git`

可选工具：

- `golangci-lint`（lint）
- `psql` / `redis-cli`（调试时直接看库）

## 二、首次跑通

```bash
git clone https://github.com/CoolBanHub/ailens360.git
cd ailens360

cp .env.example .env       # 填好 AILENS360_JWT_SECRET / DB_DSN / REDIS_ADDR
go mod download
make build                 # → ./bin/ailens360
make run                   # 启动后端，监听 :8080
```

前端：

```bash
cd frontend
pnpm install
pnpm dev                   # → http://localhost:5173
```

如果跨域被拦，把 `http://localhost:5173` 加入后端 `AILENS360_API_CORS_ORIGINS`（逗号分隔）。

## 三、目录速览

完整说明见 [`project-structure.md`](./project-structure.md)。常用入口：

- `cmd/ailens360/main.go` —— 程序入口，仅 CLI / signal 处理
- `internal/app/app.go` —— 组件装配（手写 DI）
- `internal/proxy/handler.go` —— 反向代理核心 + REDACT
- `internal/proxy/stream/*` —— SSE 解析器（openai / anthropic / gemini）
- `internal/api/router.go` —— REST 路由总表
- `internal/storage/postgres/migrations/` —— 所有 DB 迁移
- `frontend/src/pages/` —— 前端页面
- `docs/` —— 设计文档

## 四、代码规范

### 4.1 Go

- `gofmt` / `goimports` 强制（建议在 IDE 上保存即格式化）
- `go vet ./...` 必须通过；`make lint` 等价
- 优先用标准库；引入新依赖前请在 PR 描述里说明理由
- **不引入大型 Web 框架**（Gin / Kratos 等），路由仍走 `chi`
- 错误处理：直接 `return err`，不要吞掉；对外接口统一 JSON 错误格式（见 `internal/api/response`）
- 日志统一用 `log/slog`，**严禁** `fmt.Println` 进生产代码路径
- 仓储接口先动 `internal/storage/repo/types.go`，再补 Postgres 实现

### 4.2 TypeScript / React

- TypeScript 严格模式
- 组件库：HeroUI + Tailwind，避免再引入新的 UI 库
- 状态管理：TanStack Query 管远程数据，本地 state 用原生 hooks
- 与后端的 API client 集中在 `frontend/src/api/`

### 4.3 测试

- 新增 / 修改业务代码 **必须**附带单测
- 关键测试位置：
  - `internal/proxy/handler_test.go`
  - `internal/proxy/stream/openai_parser_test.go`
  - `internal/api/middleware/middleware_test.go`
  - `internal/auth/auth_test.go`
- 跑全部：`make test`（即 `go test ./...`）

### 4.4 DB 迁移

- 文件命名：`{n}_{slug}.up.sql` + `{n}_{slug}.down.sql`，二者一一对应
- 放在 `internal/storage/postgres/migrations/`
- 新增字段优先用 `DEFAULT` 兼容历史行
- 同步更新 [`architecture.md` §五 数据模型](./architecture.md#五数据模型)

## 五、提交规范

### 5.1 Branch

- 直接从 `main` fork / 拉新分支：`feat/short-description` / `fix/issue-123` / `docs/xxx`
- **不要**直接往 `main` push

### 5.2 Commit Message

简洁、动词开头、单行 ≤ 72 字符。示例：

```
feat(proxy): support upstream URL with port in path
fix(stream): handle anthropic content_block_stop without delta
docs(architecture): document tags column comma-separated convention
```

每个 commit 应能独立编译并通过 `make test`。

### 5.3 PR 描述模板

```
## 改了什么 / 为什么

（一两句话说清楚动机）

## 影响面

- 用户感知：
- API 兼容：
- DB 迁移：

## 验证

- [ ] make test
- [ ] make lint
- [ ] 手动验证步骤 …
```

PR 标题用与 Commit 一致的风格。

### 5.4 PR Review

- 由维护者审阅，可能要求多轮修改
- 期望响应时间内（约 1 周）会给出第一次反馈
- 合并前 squash，保持 `main` 历史清晰

## 六、Issue 流程

Issue 类型：

- **Bug**：写清楚版本、复现步骤、期望/实际行为、日志片段（**剔除敏感 Header**）
- **Feature**：先说"想解决什么问题"，再说"想怎么做"。避免 "Please add X" 这种无背景请求
- **Question**：欢迎，但请先翻 [`docs/`](.) 看是否已有答案

请勿在 Issue 中粘贴真实的上游 API Key、JWT、`Authorization` Header。AILens360 自身已在 trace 中对这些做 REDACT，但 GitHub Issue 没有这层保护。

## 七、文档贡献

- 设计文档全部在 `docs/` 下
- 修改任何文档时一并更新 `docs/README.md` 索引
- 中文是主语言；将来加英文文档时建议挂在 `docs/en/` 下镜像
- 不要为了"显得完整"加注释或文档；用户能看清楚就够了

## 八、行为准则

- 友好、就事论事，不要 ad hominem
- 安全相关问题（敏感数据泄露 / 鉴权绕过 / DoS）**不要**直接开 public issue，先邮件联系维护者私下披露

## 九、License

提交即视为同意以 [Apache License 2.0](../LICENSE) 协议贡献。
