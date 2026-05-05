# API 总览

本文档提供当前后端 HTTP 接口的高层说明。

统一前缀：

- `/api/v1`

统一响应格式：

```json
{
  "code": 0,
  "message": "success",
  "data": {},
  "request_id": "..."
}
```

其中：

- `code = 0` 表示成功
- `request_id` 由中间件生成，便于排查日志

## 1. 鉴权

### `POST /api/v1/auth/register`

请求体：

```json
{
  "username": "alice",
  "password": "123456",
  "nickname": "Alice"
}
```

### `POST /api/v1/auth/login`

请求体：

```json
{
  "username": "alice",
  "password": "123456"
}
```

返回中包含：

- `token`
- `user_id`
- `username`
- `nickname`

## 2. 用户

### `GET /api/v1/users/me`

需要：

- `Authorization: Bearer <token>`

### `GET /api/v1/users/:id`

可匿名访问。

如果带登录态，会额外返回与当前 viewer 相关的关注状态。

### `GET /api/v1/users/:id/posts`

查询参数：

- `last_post_id`
- `limit`

### `GET /api/v1/users/:id/followers`

查询参数：

- `last_follow_id`
- `limit`

### `GET /api/v1/users/:id/following`

查询参数：

- `last_follow_id`
- `limit`

## 3. 帖子

### `GET /api/v1/posts/:id`

获取帖子详情。

### `POST /api/v1/posts`

需要登录。

请求体：

```json
{
  "content": "hello feed"
}
```

### `DELETE /api/v1/posts/:id`

需要登录，只允许作者删除自己的帖子。

## 4. 帖子互动

### `GET /api/v1/posts/interactions/status`

查询参数：

- `post_ids=1,2,3`

说明：

- 可选登录
- 已登录时返回当前用户对这些帖子的点赞 / 收藏状态

### `POST /api/v1/posts/:id/like`

需要登录。

### `DELETE /api/v1/posts/:id/like`

需要登录。

### `POST /api/v1/posts/:id/collect`

需要登录。

### `DELETE /api/v1/posts/:id/collect`

需要登录。

### `GET /api/v1/posts/:id/comments`

查询参数：

- `last_comment_id`
- `limit`

### `POST /api/v1/posts/:id/comments`

需要登录。

请求体：

```json
{
  "content": "nice post"
}
```

## 5. 关注关系

### `POST /api/v1/follows/:target_user_id`

需要登录。

### `DELETE /api/v1/follows/:target_user_id`

需要登录。

## 6. Feed

### `GET /api/v1/feed`

需要登录。

查询参数：

- `limit`
- `last_post_id`
- `cursor`
- `refresh=1`

说明：

- `last_post_id` 与 `cursor` 不能同时传
- 开始翻第一页时，通常不传 `cursor`
- 进入混排分页后，优先使用 `next_cursor_token`

典型返回字段：

- `items`
- `has_more`
- `next_cursor`
- `next_cursor_token`
- `fallback_mode`

### `GET /api/v1/feed/discover`

需要登录。

查询参数：

- `limit`
- `last_post_id`

说明：

- Discover Feed 当前是全站公开帖子按时间倒序

## 7. DLQ

### `GET /api/v1/feed/dlq`

需要登录，且用户必须在 `feed_dlq_operators` 中。

查询参数：

- `limit`

### `POST /api/v1/feed/dlq/replay`

需要登录，且角色需要是 `admin`。

请求体：

```json
{
  "dlq_message_id": "1740000000000-0",
  "delete_after_replay": true
}
```

## 8. 常见错误码

### 通用

- `1001`: bad request
- `1002`: unauthorized
- `1003`: internal server error
- `1005`: resource not found
- `1006`: forbidden

### 业务

- `2001`: username already exists
- `2002`: invalid username or password
- `3001`: post not found
- `3002`: already followed

## 9. 调试建议

建议在 Postman / Apifox 中维护两个环境变量：

- `base_url`
- `token`

例如：

- `base_url=http://127.0.0.1:8080/api/v1`

之后所有接口都基于这个前缀调试即可。

