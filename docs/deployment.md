# 部署指南

> **基线**：Postgres + Redis 是必备依赖。应用进程无状态，多副本水平扩只需扩 `ailens360` 容器/进程数。
> 更高阶分布式（Proxy / Collector 拆分、MQ 解耦）规划见 [`architecture.md` §八](./architecture.md#八性能与扩展)。

---

## 一、部署模式对比

| 模式 | 适用规模 | 依赖 | 维护成本 |
|---|---|---|---|
| **A. 二进制 + systemd + 外置 PG/Redis** | 个人 / 小团队 | Postgres + Redis | ★★ |
| **B. Docker Compose（推荐）** | 小团队 / 单机一键起 | Docker / Compose | ★ |
| **C. 多副本（同一 Compose 网络）** | 中等规模 / 单机 LB | Compose + LB | ★★ |
| **D. Kubernetes（Helm 规划中）** | 多节点 / 高可用 | K8s + 外置 DB | ★★★ |

> 默认开发流程：`docker compose -f docker-compose.deps.yml up -d` 只起 **Postgres + Redis** 两个依赖；
> 应用本身通过 `make build && ./bin/ailens360 server ...` 在宿主机直接跑（便于调试 / 热改）。
> 生产环境再视情况把应用容器化（Dockerfile 已就绪）；扩副本时把应用进程多起几份 + 前置 LB 即可。

---

## 二、模式 A：二进制 + systemd（推荐生产起步）

### 2.1 构建

```bash
git clone https://github.com/CoolBanHub/ailens360.git
cd ailens360

# 本机构建
make build                       # 输出 ./bin/ailens360

# 或交叉编译 Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -ldflags "-s -w" -o bin/ailens360-linux-amd64 ./cmd/ailens360
```

> 二进制无 CGO 依赖，可直接拷到任意 Linux 主机运行。

### 2.2 目录与配置

```bash
sudo mkdir -p /opt/ailens360/{bin,data}
sudo cp bin/ailens360 /opt/ailens360/bin/
sudo cp .env.example /opt/ailens360/.env
sudo chmod 600 /opt/ailens360/.env
```

编辑 `/opt/ailens360/.env`。所有配置都通过环境变量加载，没有 yaml 配置文件。常用变量：

| 变量 | 用途 |
|---|---|
| `AILENS360_AUTH_USERNAME` | 控制台登录用户名 |
| `AILENS360_AUTH_PASSWORD` | 控制台登录密码 |
| `AILENS360_JWT_SECRET` | JWT 签名密钥（**必填**，留空启动失败） |
| `AILENS360_DB_DSN` | Postgres DSN（必填） |
| `AILENS360_REDIS_ADDR` | Redis 地址（必填） |

完整变量列表见仓库根目录 `.env.example`。

### 2.3 前台验证

```bash
cd /opt/ailens360
./bin/ailens360 server   # 默认读取当前目录下的 .env
```

检查：

```bash
curl http://localhost:8080/healthz     # → ok
curl http://localhost:8080/version
```

### 2.4 systemd 单元

`/etc/systemd/system/ailens360.service`：

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

`/opt/ailens360/.env`（权限 `chmod 600`，systemd 会通过 `EnvironmentFile=` 加载，应用本身也会读同一个文件）：

```
AILENS360_AUTH_USERNAME=admin
AILENS360_AUTH_PASSWORD=<强随机串>
AILENS360_JWT_SECRET=<openssl rand -hex 32>
AILENS360_DB_DSN=postgres://...
AILENS360_REDIS_ADDR=...
```

启用：

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin ailens
sudo chown -R ailens:ailens /opt/ailens360
sudo chmod 600 /opt/ailens360/.env
sudo systemctl daemon-reload
sudo systemctl enable --now ailens360
sudo journalctl -u ailens360 -f
```

### 2.5 升级与回滚

```bash
# 升级
sudo systemctl stop ailens360
sudo cp bin/ailens360 /opt/ailens360/bin/ailens360
sudo systemctl start ailens360
# 启动时会自动 apply 新增的 Postgres 迁移
```

> 跨越破坏性迁移的回滚不安全；如有 `down.sql` 不可逆字段（如 DROP COLUMN），建议先在 Postgres 侧打快照、再降级。

---

## 三、模式 B：Docker 单容器

镜像由 `deploy/docker/Dockerfile` 构建，基于 alpine，启动用户为非 root。**应用容器本身无状态**，所有持久化交给外置的 Postgres + Redis。

### 3.1 构建镜像

```bash
make docker         # 等价于 docker build -t ailens360/ailens360:latest -f deploy/docker/Dockerfile .
```

### 3.2 运行

```bash
docker run -d --name ailens360 \
  -p 8080:8080 \
  -e AILENS360_AUTH_USERNAME=admin \
  -e AILENS360_AUTH_PASSWORD="$(openssl rand -base64 24)" \
  -e AILENS360_JWT_SECRET="$(openssl rand -hex 32)" \
  -e AILENS360_DB_DSN="postgres://ailens:***@host.docker.internal:5432/ailens360?sslmode=disable" \
  -e AILENS360_REDIS_ADDR="host.docker.internal:6379" \
  --restart unless-stopped \
  ailens360/ailens360:latest
```

> 跨容器访问宿主机 Postgres / Redis 时，Linux 下 `host.docker.internal` 需要 `--add-host=host.docker.internal:host-gateway` 才能解析。

### 3.3 自定义配置

挂载一个 `.env` 文件即可（容器内默认从 `/app/.env` 读取）：

```bash
docker run -d --name ailens360 \
  -p 8080:8080 \
  -v $PWD/.env:/app/.env:ro \
  ailens360/ailens360:latest
```

也可以完全用 `-e` / `--env-file` 直接注入环境变量，不挂载文件。

---

## 四、模式 C：Docker Compose

仓库根目录提供两份 compose 文件：

| 文件 | 内容 | 适用场景 |
|---|---|---|
| `docker-compose.deps.yml` | 仅 Postgres + Redis | 本地开发：依赖跑容器、应用用 `make run` 在宿主机跑 |
| `docker-compose.yml` | 应用（从源码构建） + Postgres + Redis | 一台服务器一键自部署 |

```bash
# 仅依赖（应用本机跑）
docker compose -f docker-compose.deps.yml up -d

# 全栈一键起：先写 .env（AILENS360_AUTH_PASSWORD 与 AILENS360_JWT_SECRET 必填）
cat > .env <<EOF
AILENS360_AUTH_USERNAME=admin
AILENS360_AUTH_PASSWORD=$(openssl rand -base64 24)
AILENS360_JWT_SECRET=$(openssl rand -hex 32)
EOF
docker compose up -d --build
docker compose logs -f ailens360
```

或在仓库根目录直接：

```bash
make docker-up       # 等价于 docker compose -f docker-compose.deps.yml up -d
make docker-down
```

---

## 五、反向代理（生产强烈推荐）

### 5.1 Nginx

注意：**SSE 必须关闭 buffering、放宽超时**，否则首 token 延迟会非常糟糕。

```nginx
server {
    listen 443 ssl http2;
    server_name ailens.example.com;
    # ssl_certificate / ssl_certificate_key 配置略

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
}
```

### 5.2 Caddy

```
ailens.example.com {
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1
        transport http {
            read_timeout 1h
            write_timeout 1h
        }
    }
}
```

`flush_interval -1` 关闭 buffering，等价于 Nginx 的 `proxy_buffering off`。

### 5.3 路径分流（高级）

若想把代理流量与控制台 API 走不同上游 / 不同限流策略：

- `/p/` → 代理后端
- `/api/` → 控制台后端
- `/`（含 `/healthz` / `/version`） → 任意

v0.1 单进程同端口同时承载这三类流量；模式 D（K8s）可启动两份进程，分别只暴露其中一类路径。

---

## 六、Kubernetes（规划）

v0.4 计划提供官方 Helm chart，关键 Pod 设计：

- **proxy-deployment**：N 副本，对等无状态，承载 `/p/` 路径
- **api-deployment**：M 副本，承载 `/api/` 路径
- **collector-deployment**（v0.4+）：消费 NATS 队列，批量写存储
- 共享 PostgreSQL（元数据） + ClickHouse（trace 明细，v0.4+） + Redis（缓存 + 限流）

`terminationGracePeriodSeconds` 至少 90s，保证长 SSE 连接优雅收尾。

---

## 七、配置

所有配置都通过环境变量加载，没有 yaml。完整字段见 `.env.example` 与 `internal/config/config.go`。

常用环境变量速查：

| 变量 | 默认 | 说明 |
|---|---|---|
| `AILENS360_AUTH_USERNAME` | `admin` | 控制台登录用户名 |
| `AILENS360_AUTH_PASSWORD` | `admin` | 控制台登录密码 |
| `AILENS360_JWT_SECRET` | —（**必填，留空启动失败**） | JWT 签名密钥 |
| `AILENS360_DB_DSN` | — | Postgres DSN，例如 `postgres://ailens:***@127.0.0.1:5432/ailens360?sslmode=disable` |
| `AILENS360_REDIS_ADDR` | — | Redis 地址，例如 `127.0.0.1:6379` |

---

## 八、备份与恢复

应用进程无状态，所有持久化在 Postgres：

```bash
# 在线 dump（不需停服务）
pg_dump --format=custom --no-owner --no-privileges \
  -d "$AILENS360_DB_DSN" \
  -f /backup/ailens360-$(date +%Y%m%d).dump

# 恢复
pg_restore --clean --if-exists --no-owner --no-privileges \
  -d "$AILENS360_DB_DSN" /backup/ailens360-YYYYMMDD.dump
```

Redis 只承担缓存与实时计数，可丢失重建——无需常规备份。

请求 / 响应原文直接落 `traces` 表的 TEXT 列。若数据库膨胀过快，可调小 `collector.raw_body_limit`（默认 256 KiB）或定期按 `created_at` 清理老 trace。

---

## 九、监控

- `GET /healthz` —— 给 LB 健康检查用
- `GET /version` —— 版本号
- 日志：默认 `text` 格式输出到 stdout；改成 `log.format: json` 后可被 Promtail / Vector 直接摄入
- Prometheus `/metrics` 端点暂未实现（v0.4+ 路线图）

---

## 十、安全清单（生产前必做）

- 不要保留 `AILENS360_AUTH_PASSWORD=admin`
- 设置 `AILENS360_JWT_SECRET` 为强随机串
- 在前端反向代理上启用 HTTPS（避免 Authorization 在 TLS 外暴露）
- 防火墙限制 `/api/` 路径只对受信 IP 段开放（`/p/` 路径需要对应用网络开放）
- 备份策略落地（至少每日一次 `pg_dump`）
- 详细安全模型见 [`architecture.md` §四](./architecture.md#四安全模型)
