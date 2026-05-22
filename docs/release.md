# 发布到 Docker Hub / GitHub

这套仓库现在支持两种分发：

- Docker Hub：`coolbanhub/ailens360:<tag>` 与 `coolbanhub/ailens360:latest`
- GHCR：`ghcr.io/<owner>/ailens360:<tag>` 与 `latest`

触发方式在 [`.github/workflows/release.yml`](../.github/workflows/release.yml)：

- `git push origin v0.0.1`：自动发布
- GitHub Actions 页面手动触发 `Release`，并填入已有 tag

## 一、Docker Hub 需要怎么配

先在 Docker Hub 创建仓库：

1. 登录 Docker Hub
2. 新建 repository：`ailens360`
3. 记住你的 namespace，例如 `coolbanhub`

然后创建 Access Token：

1. 进入 Docker Hub `Account settings`
2. 打开 `Personal access tokens`
3. 新建一个 token，权限给 `Read, Write, Delete` 或至少 `Read, Write`
4. 保存这个 token，只会显示一次

## 二、GitHub 仓库需要怎么配

进入你的 GitHub 仓库：

1. 打开 `Settings` -> `Secrets and variables` -> `Actions`
2. 新建两个 repository secrets：
   - `DOCKERHUB_USERNAME`：你的 Docker Hub 用户名或组织名
   - `DOCKERHUB_TOKEN`：上一步创建的 Docker Hub access token

`GITHUB_TOKEN` 不需要自己创建，Actions 会自动提供，用来推 GHCR 和创建 GitHub Release。

如果仓库属于组织，还需要确认：

1. `Settings` -> `Actions` -> `General`
2. `Workflow permissions` 设为 `Read and write permissions`

## 三、怎么发版

推荐用 annotated tag：

```bash
git tag -a v0.0.1 -m "v0.0.1

- first public docker release
- add compose deployment
- add docker hub workflow"
git push origin v0.0.1
```

推送后 workflow 会做这些事：

1. 构建多架构镜像 `linux/amd64` 和 `linux/arm64`
2. 推送到 Docker Hub
3. 推送到 GHCR
4. 用 tag message 创建 GitHub Release

## 四、部署时怎么用

默认 compose 直接拉发布镜像：

```bash
echo "AILENS360_JWT_SECRET=$(openssl rand -hex 32)" > .env
docker compose up -d
```

如果你要指定版本：

```bash
echo "AILENS360_IMAGE=coolbanhub/ailens360:0.0.1" >> .env
docker compose up -d
```

如果你还没发 Docker Hub，想先在本地从源码构建：

```bash
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

## 五、常见问题

`pull access denied for coolbanhub/ailens360`

说明 Docker Hub 仓库还没创建，或者 workflow 还没成功推送过首个 tag。

`unauthorized: authentication required`

通常是 `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` 配错，或者 token 权限不够。

`workflow_dispatch` 手动发布失败

这个 workflow 现在要求你填写的 tag 已经存在。先本地打 tag 并 push，再手动重跑即可。
