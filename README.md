# Feed Flow System

一个用于探索社交 Feed 读写链路与工程实践的个人项目。

项目核心目标不是做“大而全”的推荐平台，而是围绕一个关注流场景，把后端里真正有技术含量的部分做出来并讲清楚：

- 用户注册、登录、JWT 鉴权
- 发帖、删帖、关注、取关
- Home Feed / Discover Feed
- Feed V1 Pull 流
- Feed V2 推拉混合
- Redis Feed 缓存与失效
- Redis Stream 异步事件链路
- Worker 消费、重试退避、XAUTOCLAIM pending 回收、DLQ
- 曝光去重、作者打散、跨页作者限流

## 项目说明

这是一个围绕关注流场景持续迭代的个人项目，重点关注：

- 业务抽象能力
- Feed 读写链路设计能力
- Redis 工程化使用能力
- 异步一致性与降级思维
- 测试护栏意识

它不是短视频推荐系统，也不是复杂排序平台，但已经覆盖了一个社交 Feed 项目里比较核心的读写与异步能力。

## 当前能力

### 已完成主链路

- 用户注册 `POST /api/v1/auth/register`
- 用户登录 `POST /api/v1/auth/login`
- 当前用户信息 `GET /api/v1/users/me`
- 用户主页、作品列表、粉丝列表、关注列表
- 发帖 / 删帖 / 帖子详情
- 点赞、收藏、评论、互动状态查询
- 关注 / 取关
- Home Feed `GET /api/v1/feed`
- Discover Feed `GET /api/v1/feed/discover`

### Feed 方向能力

- Feed V1：基于关注关系的 Pull 读取
- Feed V2：Push / Pull 混合分发
- 小作者写入粉丝 inbox，大作者走 pull-only
- Inbox + Pull 混排
- `limit+1 + has_more + next_cursor_token` 分页
- 双游标 + pending 保留，避免混排分页漏刷
- Redis Feed 读缓存
- 发帖 / 关注 / 取关 / 删帖的缓存失效与修正
- 曝光去重窗口
- 单页打散
- 跨页作者限流

### 异步可靠性

- 发帖 / 删帖后发布 Redis Stream 事件
- 独立 `cmd/worker` 消费事件
- `XREADGROUP` 消费
- `XAUTOCLAIM` 接管崩溃 consumer 的 pending
- 指数退避 + jitter，避免瞬时故障导致 worker 退出
- 重试计数器
- 超过最大重试后写入 DLQ
- DLQ 查询与重放接口

## 技术栈

- Go 1.25
- Gin
- GORM
- MySQL 8
- Redis
- React + TypeScript + Vite（前端演示页）
- Docker Compose（本地 MySQL / Redis）

## 目录结构

```text
.
├─ cmd/
│  ├─ server/      # HTTP 服务入口
│  ├─ worker/      # Feed 异步 worker
│  └─ migrate/     # GORM AutoMigrate 入口
├─ configs/        # 配置文件
├─ docs/           # 项目文档
├─ frontend/       # 前端演示页
├─ internal/
│  ├─ app/         # 应用装配与配置
│  ├─ handler/     # HTTP Handler
│  ├─ middleware/  # Gin 中间件
│  ├─ model/       # GORM 模型
│  ├─ pkg/         # 通用包
│  ├─ repository/  # DB / Redis 访问层
│  ├─ router/      # 路由注册
│  └─ service/     # 业务与 Feed 核心逻辑
├─ migrations/
└─ docker-compose.yml
```

## 快速开始

### 1. 启动 MySQL 和 Redis

```powershell
docker compose up -d mysql redis
```

默认端口：

- MySQL: `127.0.0.1:3306`
- Redis: `127.0.0.1:6379`

### 2. 检查配置

默认使用：

- `configs/config.yaml`

如需自定义配置，可以设置环境变量：

```powershell
$env:CONFIG_PATH="configs/config.yaml"
```

### 3. 执行迁移

```powershell
go run ./cmd/migrate
```

当前会自动迁移这些表：

- `users`
- `user_counts`
- `posts`
- `post_likes`
- `post_collects`
- `post_comments`
- `follows`
- `feed_dlq_operators`

### 4. 启动 HTTP 服务

```powershell
go run ./cmd/server
```

默认监听：

- `http://127.0.0.1:8080`

健康检查：

- `GET /health`
- `GET /api/v1/health`

### 5. 启动 Feed Worker

另开一个终端：

```powershell
go run ./cmd/worker
```

### 6. 运行前端演示

```powershell
cd frontend
npm install
npm run dev
```

默认前端地址：

- `http://127.0.0.1:5173`

Vite 已代理 `/api` 到 `http://127.0.0.1:8080`。

## 推荐调试顺序

1. 注册两个用户，例如 `alice` 和 `bob`
2. 分别登录拿 token
3. `bob` 关注 `alice`
4. `alice` 发帖
5. 查看 `bob` 的 `GET /api/v1/feed`
6. 启动 worker 后，观察 Redis Stream 消费与 inbox fanout
7. 测试删帖、取关后的 Feed 修正行为

## 当前接口概览

### 鉴权与用户

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `GET /api/v1/users/me`
- `GET /api/v1/users/:id`
- `GET /api/v1/users/:id/posts`
- `GET /api/v1/users/:id/followers`
- `GET /api/v1/users/:id/following`

### 帖子与互动

- `GET /api/v1/posts/:id`
- `POST /api/v1/posts`
- `DELETE /api/v1/posts/:id`
- `GET /api/v1/posts/interactions/status`
- `POST /api/v1/posts/:id/like`
- `DELETE /api/v1/posts/:id/like`
- `POST /api/v1/posts/:id/collect`
- `DELETE /api/v1/posts/:id/collect`
- `GET /api/v1/posts/:id/comments`
- `POST /api/v1/posts/:id/comments`

### 关注与 Feed

- `POST /api/v1/follows/:target_user_id`
- `DELETE /api/v1/follows/:target_user_id`
- `GET /api/v1/feed`
- `GET /api/v1/feed/discover`

### DLQ 运维接口

- `GET /api/v1/feed/dlq`
- `POST /api/v1/feed/dlq/replay`

说明：

- `feed/dlq` 需要在 `feed_dlq_operators` 表中配置操作员
- `operator` 可查看
- `admin` 可重放

更详细的接口说明见：

- [docs/api-overview.md](docs/api-overview.md)

## 核心设计摘要

### 1. Feed V1：纯 Pull

最早版本只做关注关系拉取：

- 查当前用户关注的人
- 按 `post_id desc` 拉帖子
- 用 `last_post_id` 做简单分页

优点：

- 简单
- 一致性清晰

缺点：

- 大量读时对数据库依赖更重
- 很难体现 Feed 工程化能力

### 2. Feed V2：Push / Pull 混合

按作者粉丝量分流：

- 粉丝数小于等于阈值：`push_and_pull`
- 粉丝数大于阈值：`pull_only`

写路径：

- 小作者发帖后，worker 会把 `post_id` fanout 到粉丝 inbox
- 大作者只发事件，不做大规模 push

读路径：

- 同时读取 inbox 候选和 pull 候选
- 按策略混排返回

### 3. 混排分页

混排不能只靠一个 `last_post_id`，否则会在“保留 pull 坑位 / pending 候选 / 去重”场景下出现跳项。

当前方案：

- `limit+1`
- `has_more`
- `next_cursor_token`
- inbox cursor
- pull cursor
- pending inbox ids
- pending pull ids
- recent author history

这样可以在混排、去重、跨页打散同时存在时，尽量避免重复和漏刷。

### 4. Redis 读缓存

Home Feed 返回结果支持按请求维度缓存：

- key: `feed:home:{user_id}:{cursor_or_last_post_id}:{limit}`

缓存失败时降级到数据库 / inbox 读取。

### 5. 异步链路

帖子生命周期事件会进入 Redis Stream：

- 主流：`feed:invalidation:events`
- DLQ：`feed:invalidation:dlq`

worker 负责：

- fanout inbox
- 清理删帖遗留 inbox
- 失效粉丝 feed cache
- pending reclaim
- 重试与 DLQ

更详细说明见：

- [docs/architecture.md](docs/architecture.md)

## Redis 关键数据结构

### Feed Cache

- key: `feed:home:{user_id}:...`
- value: JSON 序列化的 Feed 响应

### Feed Inbox

- key: `feed:inbox:{user_id}`
- type: ZSET
- member: `post_id`
- score: `occurred_at`

### Exposure Window

- key: `feed:exposure:{user_id}`
- type: ZSET
- member: `post_id`
- score: `seen_at`

### Stream / Retry / DLQ

- stream: `feed:invalidation:events`
- dlq stream: `feed:invalidation:dlq`
- retry key: `feed:invalidation:retry:{stream_id}`

## 测试

运行全部测试：

```powershell
go test ./...
```

项目已经为以下主链路补了较多测试护栏：

- 注册 / 登录 / JWT
- `/users/me`
- Post / Follow / Feed
- Feed 混排与分页
- Exposure 去重
- Worker 重试 / reclaim / DLQ
- Inbox repository

## 文档导航

- [docs/local-run.md](docs/local-run.md): 本地运行与联调
- [docs/architecture.md](docs/architecture.md): 架构与核心链路
- [docs/api-overview.md](docs/api-overview.md): 接口总览
- [docs/feed-v2-config.md](docs/feed-v2-config.md): Feed V2 配置说明
- [docs/feed-outbox-design.md](docs/feed-outbox-design.md): Outbox 接入设计方案
- [docs/migrate.md](docs/migrate.md): 数据迁移说明
- [frontend/README.md](frontend/README.md): 前端演示说明

## 当前已知边界

当前系统仍然有一些明确边界：

- 没有复杂推荐召回
- 没有多维排序特征引擎
- 没有消息队列中间件替代 Redis Stream
- 没有真正的多机部署、监控告警和压测数据
- 混排策略仍然以可解释性优先，而不是线上 A/B 优化优先

## 后续可继续增强的方向

- 更强的召回打散策略
- 跨页来源限流
- 更细粒度的曝光策略
- inbox 异步清理任务
- fanout pipeline / 幂等进一步优化
- 可观测性面板与压测报告
