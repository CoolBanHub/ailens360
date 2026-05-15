# AILens360 文档索引

## 核心文档

| 文档 | 内容 |
|---|---|
| [getting-started.md](./getting-started.md) | 5 分钟跑通：起三个进程 → 登录控制台 → 创建 Project → 发起调用 → 看 trace |
| [deployment.md](./deployment.md) | 三进程（proxy/collector/api）+ 反代域名 + MinIO 部署 + 备份恢复 + 安全清单 |
| [architecture.md](./architecture.md) | 系统架构（三进程 + Redis Stream + 对象存储）、流式指标、安全模型、数据模型、技术选型、关键设计决策 |
| [api-design.md](./api-design.md) | 代理协议（`/<scheme>://<upstream>` 路径 + Project 头 + Authorization 透传）、REST API、body 端点 |
| [project-structure.md](./project-structure.md) | 后端目录结构、依赖选型、为什么不用框架、Makefile 约定 |
| [contributing.md](./contributing.md) | 开发环境、代码规范、提交流程、Issue 模板 |

## 阅读顺序建议

**准备使用**：
1. [项目 README](../README.md) —— 1 分钟知道是什么
2. [getting-started.md](./getting-started.md) —— 跑通核心流程
3. [deployment.md](./deployment.md) —— 生产部署

**准备开发**：
1. [architecture.md](./architecture.md) —— 整体设计、三进程分工、安全边界、数据模型
2. [project-structure.md](./project-structure.md) —— 目录结构与代码哲学
3. [api-design.md](./api-design.md) —— 接口契约
4. [contributing.md](./contributing.md) —— 提交 PR 前的规范
