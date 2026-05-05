# 数据迁移说明

当前项目使用：

- GORM `AutoMigrate`

迁移入口：

- `cmd/migrate/main.go`

执行命令：

```powershell
go run ./cmd/migrate
```

## 当前会创建的表

- `users`
- `user_counts`
- `posts`
- `post_likes`
- `post_collects`
- `post_comments`
- `follows`
- `feed_dlq_operators`

## 说明

当前项目阶段以“快速迭代、便于个人项目推进”为目标，因此使用 `AutoMigrate` 足够。

优点：

- 本地开发启动简单
- 改模型后验证效率高
- 适合个人项目阶段

边界：

- 不适合作为复杂生产环境迁移方案
- 不适合精细控制历史 schema 演进

如果后续要向更真实的生产化方向演进，可以考虑：

1. 引入显式 SQL migration 文件
2. 区分 DDL 发布与应用发布
3. 补充索引和变更回滚策略
