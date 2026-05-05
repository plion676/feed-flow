# 本地运行指南

本文档用于帮助你在本地完整跑起：

- MySQL
- Redis
- 后端 HTTP 服务
- Feed Worker
- 前端演示页面

## 1. 环境要求

- Go 1.25+
- Node.js 18+
- npm
- Docker Desktop / Docker Engine

## 2. 启动基础依赖

在项目根目录执行：

```powershell
docker compose up -d mysql redis
```

默认容器：

- `feed-flow-mysql`
- `feed-flow-redis`

默认端口：

- MySQL: `3306`
- Redis: `6379`

## 3. 配置文件

默认配置文件：

- `configs/config.yaml`

示例配置：

- `configs/config.example.yaml`

常用字段：

```yaml
app:
  port: 8080

mysql:
  dsn: "root:password@tcp(127.0.0.1:3306)/feed_flow?charset=utf8mb4&parseTime=True&loc=Local"

redis:
  addr: "127.0.0.1:6379"

jwt:
  secret: "replace-with-your-own-secret"
```

如果要切换配置文件：

```powershell
$env:CONFIG_PATH="configs/config.yaml"
```

## 4. 执行数据库迁移

```powershell
go run ./cmd/migrate
```

当前会自动创建这些表：

- `users`
- `user_counts`
- `posts`
- `post_likes`
- `post_collects`
- `post_comments`
- `follows`
- `feed_dlq_operators`

## 5. 启动后端服务

```powershell
go run ./cmd/server
```

默认地址：

- `http://127.0.0.1:8080`

健康检查：

```powershell
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/api/v1/health
```

## 6. 启动 Feed Worker

另开一个终端：

```powershell
go run ./cmd/worker
```

worker 负责：

- 消费发帖 / 删帖事件
- 小作者 fanout inbox
- 删帖清理 inbox
- 失效粉丝 feed cache
- 回收 pending
- 重试失败写入 DLQ

## 7. 启动前端演示

```powershell
cd frontend
npm install
npm run dev
```

默认地址：

- `http://127.0.0.1:5173`

Vite 已将 `/api` 代理到：

- `http://127.0.0.1:8080`

## 8. 推荐联调流程

### 8.1 注册两个用户

示例：

- `alice`
- `bob`

接口：

```http
POST /api/v1/auth/register
Content-Type: application/json

{
  "username": "alice",
  "password": "123456",
  "nickname": "Alice"
}
```

### 8.2 登录拿 token

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "username": "alice",
  "password": "123456"
}
```

返回中会带：

- `token`
- `user_id`
- `username`
- `nickname`

### 8.3 关注

假设 `bob` 关注 `alice`：

```http
POST /api/v1/follows/1
Authorization: Bearer <bob-token>
```

### 8.4 发帖

```http
POST /api/v1/posts
Authorization: Bearer <alice-token>
Content-Type: application/json

{
  "content": "hello feed"
}
```

### 8.5 查看 Feed

```http
GET /api/v1/feed?limit=10
Authorization: Bearer <bob-token>
```

## 9. 常见问题

### 9.1 `go run ./cmd/server` 启动失败

优先检查：

- MySQL 是否已启动
- Redis 是否已启动
- `configs/config.yaml` 中的 dsn / redis 地址是否正确

### 9.2 前端打不开 API

确认：

- 后端服务已启动在 `127.0.0.1:8080`
- `frontend/vite.config.ts` 中代理仍是 `/api -> 127.0.0.1:8080`

### 9.3 Feed 没有看到 fanout 效果

确认：

- worker 已启动
- `feed.hybrid.push_follower_threshold` 不是 `0`
- 发帖作者粉丝数没有超过阈值

### 9.4 DLQ 接口没有权限

DLQ 接口需要在 `feed_dlq_operators` 表里手动插入操作员。

角色说明：

- `operator`: 可查看 DLQ
- `admin`: 可重放 DLQ

