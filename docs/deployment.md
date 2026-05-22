# 部署指南

> **基线**：Postgres + Redis + MinIO（或任意 S3 兼容对象存储）三套依赖。应用由三个无状态进程组成 —— `proxy` / `collector` / `api`，共享同一个二进制。

---

## 一、部署模式对比

| 模式 | 适用规模 | 依赖 | 维护成本 |
|---|---|---|---|
| **A. 二进制 + systemd + 外置依赖** | 个人 / 小团队 | Postgres + Redis + MinIO | ★★ |
| **B. Docker Compose（推荐）** | 小团队 / 单机一键起 | Docker / Compose | ★ |
| **C. 多副本（同一 Compose 网络）** | 中等规模 / 单机 LB | Compose + LB | ★★ |
| **D. Kubernetes** | 多节点 / 高可用 | K8s + 外置 DB | ★★★ |

> 默认开发流程：`docker compose -f docker-compose.deps.yml up -d` 只起 **Postgres + Redis + MinIO** 三个依赖；
> 应用本身通过 `make build && make run` 在宿主机直接跑（便于调试 / 热改）。
> 生产环境再视情况把应用容器化（Dockerfile 已就绪）；扩副本时把每个 role 多起几份 + 前置 LB 即可。

---

## 二、模式 A：二进制 + systemd

### 2.1 三个进程角色

| Role | 命令 | 端口 | 职责 |
|---|---|---|---|
| **proxy** | `ailens360 proxy` | `:8080` | 反向代理 + body 上传 + XADD 到 Redis Stream |
| **collector** | `ailens360 collector` | `:8082` (健康检查) | XREADGROUP 消费 + 入库 + 分区维护；**owns migrations** |
| **api** | `ailens360 api` | `:8081` | REST 控制台 + 静态 UI + pricing refresher |

### 2.2 构建

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

### 2.3 目录与配置

```bash
sudo mkdir -p /opt/ailens360/{bin,data}
sudo cp bin/ailens360 /opt/ailens360/bin/
sudo cp .env.example /opt/ailens360/.env
sudo chmod 600 /opt/ailens360/.env
```

编辑 `/opt/ailens360/.env`。所有配置都通过环境变量加载，没有 yaml 配置文件。**关键变量**：

| 变量 | 用途 | 哪个 role 用 |
|---|---|---|
| `AILENS360_DB_DSN` | Postgres DSN（必填） | 全部 |
| `AILENS360_REDIS_ADDR` | Redis 地址（必填） | 全部 |
| `AILENS360_BODY_STORE_ENDPOINT` | S3/MinIO endpoint（必填） | 全部 |
| `AILENS360_BODY_STORE_BUCKET` | bucket 名（必填） | 全部 |
| `AILENS360_BODY_STORE_ACCESS_KEY_ID` / `_SECRET_ACCESS_KEY` | 凭证（必填） | 全部 |
| `AILENS360_AUTH_USERNAME` | 控制台登录用户名 | api |
| `AILENS360_AUTH_PASSWORD` | 控制台登录密码 | api |
| `AILENS360_JWT_SECRET` | JWT 签名密钥（**仅 api 必填，留空启动失败**） | api |
| `AILENS360_PUBLIC_URL` | 共享公网 origin（控制台展示用） | api |
| `AILENS360_PROXY_PUBLIC_URL` | proxy 专属覆盖（与 api 拆域名时） | api |

完整变量列表见仓库根目录 `.env.example`。

> Validate 是 role-aware 的：proxy 与 collector 不需要 `AILENS360_JWT_SECRET`，留空也能起。

### 2.4 前台验证

```bash
cd /opt/ailens360
# 先起 collector（它跑 migrations 和分区维护）
./bin/ailens360 collector &
sleep 2
./bin/ailens360 api &
./bin/ailens360 proxy
```

检查：

```bash
curl http://localhost:8080/healthz   # proxy
curl http://localhost:8081/healthz   # api
curl http://localhost:8082/healthz   # collector
```

### 2.5 systemd 三个 unit

`/etc/systemd/system/ailens360-collector.service`：

```ini
[Unit]
Description=AILens360 collector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ailens
Group=ailens
WorkingDirectory=/opt/ailens360
EnvironmentFile=/opt/ailens360/.env
ExecStart=/opt/ailens360/bin/ailens360 collector
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

`ailens360-proxy.service` 和 `ailens360-api.service` 同模板，仅 `Description` 与 `ExecStart` 末尾的子命令不同（`proxy` / `api`），并加 `After=ailens360-collector.service` 让 collector 先起（保证 schema 已迁移、分区已建）。

`/opt/ailens360/.env`（权限 `chmod 600`，所有进程共用同一份）：

```
AILENS360_DB_DSN=postgres://...
AILENS360_REDIS_ADDR=...
AILENS360_BODY_STORE_ENDPOINT=...
AILENS360_BODY_STORE_BUCKET=ailens360-traces
AILENS360_BODY_STORE_ACCESS_KEY_ID=...
AILENS360_BODY_STORE_SECRET_ACCESS_KEY=...
AILENS360_AUTH_USERNAME=admin
AILENS360_AUTH_PASSWORD=<强随机串>
AILENS360_JWT_SECRET=<openssl rand -hex 32>
AILENS360_PUBLIC_URL=https://ailens.example.com
```

启用：

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin ailens
sudo chown -R ailens:ailens /opt/ailens360
sudo chmod 600 /opt/ailens360/.env
sudo systemctl daemon-reload
sudo systemctl enable --now ailens360-collector ailens360-proxy ailens360-api
sudo systemctl status 'ailens360-*'
sudo journalctl -u ailens360-proxy -f
```

### 2.6 升级与回滚

```bash
# 升级：替换二进制后重启三个进程，collector 启动时会跑新迁移
sudo systemctl stop ailens360-proxy ailens360-api ailens360-collector
sudo cp bin/ailens360 /opt/ailens360/bin/ailens360
sudo systemctl start ailens360-collector ailens360-proxy ailens360-api
```

> 跨越破坏性迁移的回滚不安全；如有 `down.sql` 不可逆字段（如 DROP COLUMN），建议先在 Postgres 侧打快照、再降级。

---

## 三、模式 B：Docker Compose（推荐）

仓库根目录提供两份 compose 文件：

| 文件 | 内容 | 适用场景 |
|---|---|---|
| `docker-compose.deps.yml` | Postgres + Redis + MinIO | 本地开发：依赖跑容器、应用 `make run` 在宿主机跑 |
| `docker-compose.yml` | 三个应用进程 + Postgres + Redis + MinIO | 一台服务器一键自部署 |

```bash
# 仅依赖（应用本机跑）
docker compose -f docker-compose.deps.yml up -d

# 全栈一键起：先写 .env（AILENS360_JWT_SECRET 必填）
cat > .env <<EOF
AILENS360_AUTH_PASSWORD=$(openssl rand -base64 24)
AILENS360_JWT_SECRET=$(openssl rand -hex 32)
EOF
docker compose up -d
docker compose logs -f
```

```bash
# 全栈 compose（发布镜像）
make docker-up
make docker-down

# 全栈 compose（本地源码构建）
make docker-build-up
make docker-build-down

# 仅依赖（开发模式）
make docker-deps-up
make docker-deps-down
```

`docker-compose.yml` 已经把三个角色（proxy / collector / api）和 MinIO 全部配好：proxy 暴露 `:8080`、api 暴露 `:8081`、MinIO `:9000` + console `:9001`，collector 只在内部网络。

---

## 四、反向代理（生产强烈推荐）

**两个域名（或同域名 + 路径分流）**：proxy 与 api 分别监听不同端口，反代前应当区别对待。SSE 必须关闭 buffering、放宽超时。

> 更完整的 nginx 反代教程见 [`nginx.md`](./nginx.md)，包含同域名 path 分流、双域名、`merge_slashes off` 坑、SSL 申请、MinIO 反代、故障排查。

### 4.1 Nginx — 双域名

```nginx
# 代理域名 → proxy 进程（LLM 流量入口）
server {
    listen 443 ssl http2;
    server_name ailens-proxy.example.com;

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

# 控制台域名 → api 进程
server {
    listen 443 ssl http2;
    server_name ailens-console.example.com;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

对应的 `.env`：

```
AILENS360_PROXY_PUBLIC_URL=https://ailens-proxy.example.com
AILENS360_PUBLIC_URL=https://ailens-console.example.com
```

（PROXY_PUBLIC_URL 覆盖共享的 PUBLIC_URL，api 控制台返回的 `proxy_prefix` 会指向 proxy 域名。）

### 4.2 Caddy — 双域名

```
ailens-proxy.example.com {
    reverse_proxy 127.0.0.1:8080 {
        flush_interval -1
        transport http {
            read_timeout 1h
            write_timeout 1h
        }
    }
}

ailens-console.example.com {
    reverse_proxy 127.0.0.1:8081
}
```

`flush_interval -1` 关闭 buffering，等价于 Nginx 的 `proxy_buffering off`。

### 4.3 同域名 + 路径分流（可选）

实操上 proxy 路径形如 `/https://upstream/...` —— 第一段就是 `http://` 或 `https://`，几乎不会撞库到 api 的 `/api/*`。同域名可以这么配：

```nginx
server {
    server_name ailens.example.com;
    location /api/    { proxy_pass http://127.0.0.1:8081; }
    location /healthz { proxy_pass http://127.0.0.1:8081; }
    location /        { proxy_pass http://127.0.0.1:8080; ... SSE 参数 ... }
}
```

但这种部署下 api 进程也需要承担静态前端 + body 转发流量，请谨慎评估带宽。

### 4.4 MinIO 暴露策略

- **默认（推荐）**：api 进程流式转发 body，MinIO 留在内网，**不需要单独暴露**
- **PRESIGN_REDIRECT=true**：浏览器需要能直接访问 MinIO，给 MinIO 也挂个反代域名 `s3.example.com → minio:9000`，再设 `AILENS360_BODY_STORE_PUBLIC_ENDPOINT=https://s3.example.com`，并开 MinIO CORS

---

## 五、Kubernetes

未提供官方 Helm chart，但架构天然适配。关键 Pod 设计：

- **proxy-deployment**：N 副本，对等无状态，`Service` 暴露 `:8080`
- **api-deployment**：M 副本，对等无状态，`Service` 暴露 `:8081`
- **collector-deployment**：K 副本（同一 consumer group 自动分片），只暴露 `/healthz` `:8082`
- **minio-statefulset** 或外置 S3
- 共享 PostgreSQL（带分区表）+ Redis（缓存 + Pub/Sub + Stream）

部署提示：

- `terminationGracePeriodSeconds` 至少 90s，让 proxy 长 SSE 连接优雅收尾
- collector 副本可以多开；XAUTOCLAIM 会把死掉副本的消息自动转移给活的
- collector 默认保留 3 个月 trace（`AILENS360_PARTITION_RETENTION_MONTHS=3`），到期自动 DROP 老分区并清掉 MinIO 上的对应对象；可调大或设负数关闭

---

## 六、配置

所有配置都通过环境变量加载，没有 yaml。完整字段见 `.env.example` 与 `internal/config/config.go`。

常用环境变量速查：

| 变量 | 默认 | 说明 |
|---|---|---|
| `AILENS360_DB_DSN` | — | Postgres DSN |
| `AILENS360_REDIS_ADDR` | — | Redis 地址 |
| `AILENS360_BODY_STORE_ENDPOINT` | — | S3/MinIO endpoint（host:port） |
| `AILENS360_BODY_STORE_BUCKET` | `ailens360-traces` | bucket 名 |
| `AILENS360_BODY_STORE_PRESIGN_REDIRECT` | `false` | true=302 给浏览器，false=API 流式转发 |
| `AILENS360_AUTH_USERNAME` | `admin` | 控制台登录用户名 |
| `AILENS360_AUTH_PASSWORD` | `admin` | 控制台登录密码 |
| `AILENS360_JWT_SECRET` | —（**api 必填，留空启动失败**） | JWT 签名密钥 |
| `AILENS360_PUBLIC_URL` | 空 | 共享公网 origin（控制台用） |
| `AILENS360_PROXY_PUBLIC_URL` | 空（回退 PUBLIC_URL） | proxy 专属覆盖 |
| `AILENS360_PARTITION_PRE_CREATE` | `1` | 提前建 N 个未来月分区 |
| `AILENS360_PARTITION_RETENTION_MONTHS` | `3` | >0 硬删超龄分区（DROP）并清理对应 MinIO 对象；负值关闭 |

---

## 七、备份与恢复

应用进程无状态，数据分散在三处：

```bash
# 1. Postgres：trace 元数据、project 配置
pg_dump --format=custom --no-owner --no-privileges \
  -d "$AILENS360_DB_DSN" \
  -f /backup/ailens360-pg-$(date +%Y%m%d).dump

pg_restore --clean --if-exists --no-owner --no-privileges \
  -d "$AILENS360_DB_DSN" /backup/ailens360-pg-YYYYMMDD.dump

# 2. MinIO/S3：请求/响应正文。生产环境直接用对象存储的版本控制 + 跨区域复制更稳。
#    本地 MinIO 备份示例：
mc mirror minio/ailens360-traces /backup/ailens360-bodies/

# 3. Redis 只承担缓存、Pub/Sub、实时计数、Stream 队列，可丢失重建——无需常规备份。
#    Stream 中堆积的待处理 trace 会随 Redis 数据丢失，对历史落库无影响。
```

### 7.1 控制 PG 体积

- `traces` 已按月分区。默认 `AILENS360_PARTITION_RETENTION_MONTHS=3`，collector 维护作业每 24h 检查一次，DROP 早于该窗口的 `traces_YYYYMM` 分区；想保留更久就调大（如 `12`），想完全关闭就设 `-1`。
- 大 body 在 MinIO，PG 主表瘦身。

### 7.2 控制 MinIO 体积

- 由 collector 维护作业按月与 PG 分区同步清理：DROP `traces_YYYYMM` 时会删除每个项目下的 `{project_id}/{YYYYMM}/` 前缀对象，无需额外配 Lifecycle policy。
- 若要保留 PG 中的 trace 元数据但提早清理 MinIO，可在 bucket 级再叠一层 MinIO Lifecycle（key 已按 `{project}/{YYYYMM}/{trace}/...` 分桶）。
- 若 `AILENS360_BODY_STORE_GZIP_BODIES=true`（默认开），对象本身已 gzip。

---

## 八、监控

- `/healthz` —— 三个进程都暴露，给 LB / K8s probe 用
- 日志：默认 `text` 格式输出到 stdout；改成 `AILENS360_LOG_FORMAT=json` 后可被 Promtail / Vector 直接摄入
- Redis Stream 队列深度：监控 `XLEN ailens360:traces` —— 长期堆积说明 collector 跟不上
- Prometheus `/metrics` 端点暂未实现（路线图）

---

## 九、安全清单（生产前必做）

- 不要保留 `AILENS360_AUTH_PASSWORD=admin`
- 设置 `AILENS360_JWT_SECRET` 为强随机串（openssl rand -hex 32）
- 在前端反向代理上启用 HTTPS（避免 Authorization 在 TLS 外暴露）
- 防火墙限制 api 控制台域名只对受信 IP 段开放；proxy 域名按业务需要决定
- MinIO 凭证不要用默认 `minioadmin/minioadmin`；如果走 PRESIGN_REDIRECT，把 MinIO 反代 + CORS 配好
- 备份策略落地（至少每日一次 `pg_dump` + MinIO 跨区复制）
- 详细安全模型见 [`architecture.md` §四](./architecture.md#四安全模型)
