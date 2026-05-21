# Nginx 配置指南

> 适用于**自管 nginx**（apt/yum 装的、OpenResty、aliyun 自带等）反代 ailens360 的场景。如果你用的是 Nginx Proxy Manager / 1Panel / 宝塔等 GUI 工具，请在它们的 "Advanced" / "自定义配置" 字段里粘贴本文里的 nginx 片段（同样需要 server 级的 `merge_slashes off;`）。

## 一、前置约定

本文假设你已经按 [`deployment.md`](./deployment.md) 把三个进程 + Postgres / Redis / MinIO 跑起来了。后端默认端口分布：

| Role | 监听端口 | 对外性质 |
|---|---|---|
| proxy | `127.0.0.1:18083` | LLM 流量入口（POST 居多，含 SSE 长连接） |
| api | `127.0.0.1:18081` | REST + 静态 UI + body 取回 |
| collector | `127.0.0.1:18082` | 仅 `/healthz` 内网健康检查，**不需要反代** |
| MinIO API | `127.0.0.1:19002` | 仅当走 PRESIGN_REDIRECT 模式时才需要对外暴露 |

如果你的端口和上述不同，把示例里的端口换成你的实际值即可。

## 二、关键陷阱：`merge_slashes`

ailens360 的代理 URL 形态是：

```
POST /https://api.openai.com/v1/chat/completions
```

注意路径里**有 `://`**。nginx 默认开启 [`merge_slashes on`](https://nginx.org/en/docs/http/ngx_http_core_module.html#merge_slashes)，会把相邻的多个 `/` 折成一个 —— `/https://...` 会被改成 `/https:/...`，导致路径里的 scheme 信息丢失，proxy 解析失败、location 匹配失败。

**结论：服务 ailens360 的 server 块里必须显式 `merge_slashes off;`**。这是与所有其它常规反代场景最大的不同。

## 三、推荐方案：同域名 + path 分流

一个域名（例如 `ailens.example.com`）同时承担控制台访问和 LLM 流量。利用 `^~` 前缀 location 把 `/http://` 和 `/https://` 起头的请求扔给 proxy 进程，其余全部走 api 进程。

### 3.1 完整 server 块

```nginx
# /etc/nginx/conf.d/ailens.conf

# 给客户端流量长连接更稳定的 upstream
upstream ailens360_proxy { server 127.0.0.1:18083; keepalive 32; }
upstream ailens360_api   { server 127.0.0.1:18081; keepalive 16; }

# HTTP → HTTPS 301
server {
    listen 80;
    listen [::]:80;
    server_name ailens.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name ailens.example.com;

    # ── 证书（用 certbot 申请，见 §六）─────────────────────────────────────
    ssl_certificate     /etc/letsencrypt/live/ailens.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ailens.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    # ── 关键 ★ ─────────────────────────────────────────────────────────────
    # ailens360 的代理 URL 形如 /https://api.openai.com/...
    # 默认 merge_slashes 会把 // 折成 /，必须关闭。
    merge_slashes off;

    # ── 体积上限（默认 1m 太小，覆盖 LLM 大请求 / 长 prompt）─────────────
    client_max_body_size 32m;

    # SSE 长连接：禁掉 nginx 的应答 buffering，并把超时拉到 1 小时以上
    proxy_http_version 1.1;
    proxy_set_header   Host              $host;
    proxy_set_header   X-Real-IP         $remote_addr;
    proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header   X-Forwarded-Proto $scheme;
    proxy_set_header   Connection        "";

    # ── LLM 代理流量 ★ —— /<scheme>://upstream 走 proxy 进程 ──────────────
    location ^~ /https:// {
        proxy_pass http://ailens360_proxy;
        proxy_buffering        off;     # SSE 必需
        proxy_cache            off;
        proxy_read_timeout     1h;      # 让长流式响应能跑完
        proxy_send_timeout     1h;
        proxy_request_buffering off;    # 请求 body 也不缓存（流式上传 OK）
        chunked_transfer_encoding on;
    }
    location ^~ /http:// {
        proxy_pass http://ailens360_proxy;
        proxy_buffering        off;
        proxy_cache            off;
        proxy_read_timeout     1h;
        proxy_send_timeout     1h;
        proxy_request_buffering off;
        chunked_transfer_encoding on;
    }

    # ── 控制台 & 静态 UI —— 其余一切走 api 进程 ─────────────────────────
    location / {
        proxy_pass http://ailens360_api;
        # 浏览器到 ailens360 的 body fetch 默认走流式转发（gzip 自动协商），
        # 同样不要 buffering，否则前端会感觉卡。
        proxy_buffering        off;
        proxy_read_timeout     300s;
    }
}
```

### 3.2 检查 & 重载

```bash
sudo nginx -t                   # 语法检查
sudo systemctl reload nginx     # 平滑 reload
```

### 3.3 验证

```bash
# 控制台 SPA：应返回 200 + text/html
curl -sI https://ailens.example.com/ | head -3

# 控制台 API：未带 token 时应返回 401
curl -sI https://ailens.example.com/api/auth/me | head -3

# 代理路径：未带项目密钥应返回 401 missing_project_key
#（这一步是判断 path 分流是否生效的关键 —— 405 说明被错路由到 api 了）
curl -is -X POST https://ailens.example.com/https://api.openai.com/v1/chat/completions \
  -d '{}' --max-time 5 | head -5
```

控制台返回的 `proxy_prefix` 应该等于 `https://ailens.example.com`（不带 `/p`）。客户端 SDK 可以使用 `base_url = "https://ailens.example.com/https://api.openai.com/v1"` 并加 `X-AILens-Project-Key`，也可以使用路径模式 `base_url = "https://ailens.example.com/sk-xxx/https://api.openai.com/v1"`。

## 四、替代方案：拆两个子域名

如果想把代理流量和控制台彻底拆开（独立证书、独立带宽统计、独立 WAF 策略），用两个子域名分别指向两个进程。

```nginx
# /etc/nginx/conf.d/ailens-proxy.conf —— LLM 流量
upstream ailens360_proxy { server 127.0.0.1:18083; keepalive 32; }

server {
    listen 80; listen [::]:80;
    server_name ailens-proxy.example.com;
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name ailens-proxy.example.com;

    ssl_certificate     /etc/letsencrypt/live/ailens-proxy.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ailens-proxy.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;

    merge_slashes off;                 # ★ 同样必需
    client_max_body_size 32m;

    proxy_http_version 1.1;
    proxy_set_header   Host              $host;
    proxy_set_header   X-Real-IP         $remote_addr;
    proxy_set_header   X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header   X-Forwarded-Proto $scheme;
    proxy_set_header   Connection        "";

    location / {
        proxy_pass http://ailens360_proxy;
        proxy_buffering         off;
        proxy_cache             off;
        proxy_read_timeout      1h;
        proxy_send_timeout      1h;
        proxy_request_buffering off;
        chunked_transfer_encoding on;
    }
}
```

```nginx
# /etc/nginx/conf.d/ailens-console.conf —— 控制台 + REST
upstream ailens360_api { server 127.0.0.1:18081; keepalive 16; }

server {
    listen 80; listen [::]:80;
    server_name ailens-console.example.com;
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name ailens-console.example.com;

    ssl_certificate     /etc/letsencrypt/live/ailens-console.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ailens-console.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;

    client_max_body_size 32m;          # api 也要这么大，trace 详情会大
    # api server 块**不需要** merge_slashes off —— 它根本不该收到 /http:// 形态的请求

    proxy_http_version 1.1;
    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    location / {
        proxy_pass http://ailens360_api;
        proxy_buffering    off;
        proxy_read_timeout 300s;
    }
}
```

对应 `.env`：

```bash
AILENS360_PROXY_PUBLIC_URL=https://ailens-proxy.example.com
AILENS360_PUBLIC_URL=https://ailens-console.example.com
AILENS360_API_CORS_ORIGINS=https://ailens-console.example.com
```

## 五、可选：暴露 MinIO

**默认情况下不需要做**。ailens360 的 api 进程会从 MinIO 拉字节流式转发给浏览器（即 `AILENS360_BODY_STORE_PRESIGN_REDIRECT=false`），MinIO 完全可以待在内网。

**只有**在你显式打开 `PRESIGN_REDIRECT=true`（让浏览器直拉 MinIO 节省 api 带宽）时，才需要给 MinIO 一个公开域名：

```nginx
# /etc/nginx/conf.d/ailens-s3.conf
upstream ailens360_minio { server 127.0.0.1:19002; keepalive 16; }

server {
    listen 80; listen [::]:80;
    server_name s3.example.com;
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name s3.example.com;

    ssl_certificate     /etc/letsencrypt/live/s3.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/s3.example.com/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;

    client_max_body_size 0;            # MinIO 自己管限制

    proxy_http_version 1.1;
    proxy_set_header Host              $host;
    proxy_set_header X-Real-IP         $remote_addr;
    proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # MinIO presigned URL 需要 CORS 允许浏览器跨源访问
    add_header 'Access-Control-Allow-Origin'  '$http_origin' always;
    add_header 'Access-Control-Allow-Methods' 'GET, HEAD, OPTIONS' always;
    add_header 'Access-Control-Allow-Headers' 'Authorization, Range' always;
    if ($request_method = OPTIONS) { return 204; }

    location / {
        proxy_pass http://ailens360_minio;
        proxy_buffering off;
    }
}
```

`.env` 加：

```bash
AILENS360_BODY_STORE_PRESIGN_REDIRECT=true
AILENS360_BODY_STORE_PUBLIC_ENDPOINT=https://s3.example.com
```

> 生产建议直接用 AWS S3 / 阿里云 OSS 等托管服务，跳过 MinIO 自建反代这一步。

## 六、申请 SSL 证书（Let's Encrypt + certbot）

```bash
# CentOS / RHEL 系
sudo yum install -y certbot python3-certbot-nginx
# Debian / Ubuntu
sudo apt install -y certbot python3-certbot-nginx

# 同域名方案
sudo certbot --nginx -d ailens.example.com

# 双域名方案（一次申请两个）
sudo certbot --nginx -d ailens-proxy.example.com -d ailens-console.example.com
```

certbot 会自动修改对应的 server 块加上 ssl 行 + 配置 80 → 443 跳转。注意：**certbot 改 conf 时不会动 `merge_slashes off`**，但保险起见运行完检查一遍 conf 仍包含这一行。

自动续期：

```bash
sudo systemctl enable --now certbot.timer    # 或 cron job 跑 `certbot renew`
```

## 七、客户端接入示例

域名搭好后，客户端只需要改 `base_url`：

```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-real-openai-key",                                # 真实上游 Key，原样透传
    base_url="https://ailens.example.com/https://api.openai.com/v1",
    default_headers={"X-AILens-Project-Key": "sk-<控制台拿到的 project_key>"},
)

resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "hi"}],
    stream=True,
)
for chunk in resp:
    print(chunk.choices[0].delta.content or "", end="", flush=True)
```

接 DeepSeek / Groq / Anthropic / Ollama 时仅替换 base_url 末尾的上游 URL 段（详见 [`getting-started.md`](./getting-started.md)）。

## 八、故障排查

| 现象 | 病因 | 修法 |
|---|---|---|
| `POST` 返回 `405 Method Not Allowed`，`Allow: GET, HEAD` | 流量走到了 api 进程（它对未识别路径只挂 GET/HEAD 给 SPA 用），证明 `^~ /https://` location 没匹配 | 99% 是 `merge_slashes off;` 缺了或不在正确的 server 块里。`nginx -T \| grep merge_slashes` 确认 |
| `POST` 返回 `404 project_not_found` | 流量正确路由到 proxy 了，但项目密钥错了 | 检查 `X-AILens-Project-Key` / 路径前缀 / `?sk=` 中的 `sk-...` |
| 首 token 延迟 5 秒以上 | nginx 在 buffer SSE | 检查 `proxy_buffering off;` 是否在 location 块里 |
| 流式响应中途中断 | nginx 超时 | `proxy_read_timeout` / `proxy_send_timeout` 拉到 1h+ |
| 控制台示例 URL 显示了 nginx 端口而不是域名 | 后端 `AILENS360_PUBLIC_URL` 没配 | `.env` 里设 `AILENS360_PUBLIC_URL=https://ailens.example.com`，重启 api |
| trace 详情页 body 加载失败 | api 反代 `proxy_buffering` 没关，或者 `client_max_body_size` 太小 | 见上面 location / 配置 |
| 大请求（>1MB）返回 413 | `client_max_body_size` 默认 1m | 拉大到 32m 或更高 |
| `nginx -t` 报 `unknown directive "merge_slashes"` | 极老版本 nginx（<1.0）不支持 | 升级 nginx |

调试小技巧：临时给 location 块加 `add_header X-Loc "/https://" always;`，前端 / curl 就能看到 nginx 实际命中了哪个 location。

## 九、参考

- nginx 官方文档：https://nginx.org/en/docs/http/ngx_http_core_module.html
- Let's Encrypt 与 certbot：https://certbot.eff.org/
- 后端架构与端口含义：[architecture.md](./architecture.md)
- 完整部署流程：[deployment.md](./deployment.md)
