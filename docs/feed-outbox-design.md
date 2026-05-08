# Feed Outbox Design

本文档描述 `feed-flow-system` 在当前 `Feed V2` 基础上，接入 `outbox` 的推荐设计方案。

这是一份设计文档，不代表当前代码已经全部实现。

目标是：

- 保留现有 `inbox + pull + mix + cursor token` 主体结构
- 引入作者级 `outbox`，承接大作者的 pull 数据源
- 尽量不修改外部 API，不推翻现有分页 token 语义
- 为后续实现提供一套可直接落地的边界清晰方案

## 1. 当前设计与目标设计

### 当前设计

- 小作者：`push_and_pull`
  - 发帖后通过 worker fanout 到粉丝 `inbox`
- 大作者：`pull_only`
  - 不做 fanout
  - 用户读 Feed 时，直接从 `posts` 表按关注关系列表拉取

这意味着当前大作者 pull 的数据源是：

- `posts`

### 目标设计

- 所有作者发帖后，都维护一份自己的 `outbox`
- 小作者：
  - 仍然保留 `push_and_pull`
  - 粉丝读 Feed 时，优先从 `inbox` 读小作者内容
- 大作者：
  - 仍然保留 `pull_only`
  - 粉丝读 Feed 时，不再直接扫 `posts`
  - 而是优先从作者 `outbox` 拉取，再做多路归并
- `posts`：
  - 退回为“内容真身 + fallback 数据源”

一句话概括：

- 当前：`inbox + posts pull`
- 目标：`inbox + outbox pull + posts fallback`

## 2. 非目标

这次 outbox 设计不打算同时解决下面这些问题：

- 不改 HTTP API
- 不把 Feed token 升级成“每作者游标”
- 不引入新的消息队列中间件
- 不做推荐系统意义上的复杂召回
- 不改变当前小作者 / 大作者的分流规则

## 3. Outbox 数据结构

### Redis Key

- key: `feed:outbox:{author_id}`
- type: `ZSET`
- member: `post_id`
- score: `post_id`

### 为什么使用 `post_id` 作为 score

当前项目里：

- Feed pull 的分页语义基于 `post_id`
- `next_cursor_token` 中的 `pull_last_post_id` 也是按 `post_id` 推进

因此 outbox 使用 `score=post_id` 的好处是：

1. 和现有分页语义保持一致
2. 读 outbox 时可以直接按 `post_id < cursor` 继续翻页
3. 不需要立刻升级为 `(time, post_id)` 复合游标
4. 当前项目规模下，`post_id` 作为 Redis `double` score 足够安全

### 保留长度

每个作者 outbox 保留固定长度：

- `feed:outbox:{author_id}` 最多保留 `max_items`

设计意图：

- outbox 是“作者最近发了什么”的索引层
- 不承担永久归档功能
- 更老的内容仍然可以从 `posts` 表获得

## 4. 推荐配置项

建议新增：

```yaml
feed:
  outbox:
    enabled: true
    max_items: 1000
    read_chunk_size: 32
    max_authors_per_read: 200
    db_fallback_enabled: true
```

### `feed.outbox.enabled`

- 是否开启 outbox 读写能力
- 关闭时，系统完全退回当前 `inbox + posts pull`

### `feed.outbox.max_items`

- 单作者 outbox 最多保留多少个 `post_id`

### `feed.outbox.read_chunk_size`

- 单个作者每次从 outbox 拉取多少个 `post_id`
- 用于多路归并时的单作者补货粒度

### `feed.outbox.max_authors_per_read`

- 一次 outbox pull 最多允许参与归并的作者数
- 如果超过该值，直接回退 DB pull，避免读路径过重

### `feed.outbox.db_fallback_enabled`

- outbox 读失败或超出作者数阈值时，是否允许回退到现有 `posts` pull

## 5. Repository 设计

建议新增 `FeedOutboxRepository`：

```go
type FeedOutboxRepository interface {
    AddPostToOutbox(ctx context.Context, authorUserID int64, postID int64) error
    RemovePostFromOutbox(ctx context.Context, authorUserID int64, postID int64) error
    TrimOutbox(ctx context.Context, authorUserID int64, maxItems int64) error
    ListPostIDsByCursor(ctx context.Context, authorUserID int64, maxPostID int64, limit int) ([]int64, error)
}
```

### 语义说明

#### `AddPostToOutbox`

- 向 `feed:outbox:{author_id}` 写入 `post_id`
- 使用 `ZADD`

#### `RemovePostFromOutbox`

- 从作者 outbox 删除指定 `post_id`
- 使用 `ZREM`

#### `TrimOutbox`

- 把作者 outbox 裁剪到固定长度
- 行为和 inbox trim 类似

#### `ListPostIDsByCursor`

- `maxPostID == 0`：
  - 读取最新一批
- `maxPostID > 0`：
  - 读取 `post_id < maxPostID` 的一批

返回顺序：

- 按 `post_id DESC`

## 6. 读路径辅助 Repository

为了按作者粉丝量把关注作者分成“小作者 / 大作者”，建议新增批量读取作者粉丝数的能力。

例如：

```go
type feedUserCountRepository interface {
    BatchGetFollowerCounts(ctx context.Context, userIDs []int64) (map[int64]int64, error)
}
```

数据来源：

- MySQL `user_counts`

设计意图：

- 当前分流规则仍然由 `push_follower_threshold` 决定
- 读路径需要知道每个关注作者是：
  - `push_and_pull`
  - 还是 `pull_only`

## 7. 写路径设计

## 7.1 发帖成功后的同步写入

`PostService.Create` 在 DB 落库成功后，建议新增：

1. Best-effort 失效作者自己的 Home Feed 缓存
2. Best-effort 写作者 outbox
3. Best-effort 发布 `post_created` 事件

推荐顺序：

1. `InvalidateHomeFeed(authorID)`
2. `AddPostToOutbox(authorID, postID)`
3. `TrimOutbox(authorID, maxItems)`
4. `PublishPostCreatedEvent(authorID, postID)`

### 为什么发帖后要同步 best-effort 写 outbox

因为如果 outbox 只靠 worker 异步补：

- 大作者发帖后
- worker 还没消费到事件前
- 粉丝读 Feed 时可能暂时看不到这条内容

而当前系统里，大作者虽然不写 inbox，但至少还能直接从 `posts` pull 到。

为了不让接入 outbox 后“首刷可见性”变差，推荐：

- 同步 best-effort 写 outbox
- 失败不影响主写入成功

## 7.2 发帖事件的异步补写

worker 处理 `post_created` 时，再补一次：

1. `AddPostToOutbox(authorID, postID)`
2. `TrimOutbox(authorID, maxItems)`
3. 再继续原有：
   - 小作者 fanout inbox
   - 失效粉丝 feed cache

### 为什么还要 worker 再补一遍

因为同步写 outbox 是 best-effort：

- Redis 瞬时失败时，不会让发帖失败
- 但异步 worker 能再补一次，最终把作者 outbox 修回来

这个补写是安全的，因为：

- `ZADD` 对同一个 `post_id` 是天然幂等友好的

## 7.3 删帖后的 outbox 处理

`PostService.Delete` 不需要同步删 outbox。

推荐沿用当前异步一致性思路：

1. 同步主流程：
   - `posts.status = deleted`
   - 失效作者自己的 Home Feed 缓存
   - 发布 `post_deleted` 事件
2. worker 异步：
   - `RemovePostFromOutbox(authorID, postID)`
   - 从粉丝 inbox 删除该 `post_id`
   - 失效粉丝 feed cache

### 为什么删帖可以异步删 outbox

因为当前读路径回表时只认：

- `status = published`

就算 outbox 里暂时还保留着这个 `post_id`：

- `ListByIDs(...)` 时也拿不到这个 deleted 帖子
- 读路径仍然会跳过它

所以：

- outbox cleanup 负责“让索引干净”
- 读过滤负责“让结果一定正确”

## 8. Follow / Unfollow 与 outbox 的关系

### Follow

Follow 不需要额外改 outbox。

效果是：

- 关注关系进入 DB 后
- 后续读 Feed 时，这个作者会进入当前用户的 allowed authors
- 如果该作者是大作者，就会进入 outbox pull 集合

### Unfollow

Unfollow 仍然保留当前设计：

1. 删除 follow 关系
2. 失效当前用户 Home Feed 缓存
3. best-effort 清理当前用户 inbox 中该作者历史帖子

但是：

- **不需要清 outbox**

因为 outbox 是作者级索引：

- key 在作者维度
- 不是在粉丝维度

取关后只要读路径不再把该作者列入 allowed authors：

- 这个作者 outbox 自然不会再参与当前用户的 Feed 读取

这是 outbox 相比 inbox 的一个明显优点。

## 9. 读路径总体设计

## 9.1 作者分组

当前 `GetHomeFeed` 先拿到 allowed authors：

- followings
- 自己

接入 outbox 后，推荐再把作者拆成：

- `pushAuthors`
- `pullAuthors`

分组规则：

- `follower_count <= push_follower_threshold`
  - `pushAuthors`
- `follower_count > push_follower_threshold`
  - `pullAuthors`

### 特殊规则：自己始终进 pullAuthors

当前系统里，作者自己的帖子不是靠 self inbox 提供的，而是靠 pull 路径看到。

因此在 outbox 设计里建议：

- 当前用户自己始终进入 `pullAuthors`

即使当前用户粉丝数不大，也不依赖 inbox 来看到自己发的帖子。

## 9.2 Pull 数据源选择

新的 pull 候选收集规则：

1. 如果 `feed.outbox.enabled = false`
   - 完全使用当前 `posts` pull
2. 如果 `feed.outbox.enabled = true`
   - 对 `pullAuthors` 优先走 outbox
3. 如果触发 fallback 条件
   - 回退到当前 `posts` pull`

## 9.3 Fallback 条件

推荐以下条件回退 DB pull：

1. outbox Redis 读取报错
2. `len(pullAuthors) > max_authors_per_read`
3. outbox 功能开关关闭

这里不建议把“候选不足”本身直接视为 fallback 条件。

原因是：

- 如果所有作者都正确维护了 outbox
- 候选不足通常意味着“本来就没有更多内容”
- 不是 outbox 自身故障

否则会引入两套数据源同时参与，增加重复与一致性复杂度。

## 10. Outbox Pull 的核心难点

最重要的点不是“存一份 outbox”，而是：

- **如何正确地从多个作者 outbox 中拉出全局最新的一批帖子**

不能使用这种错误做法：

- 每个作者随便取前 1~2 条
- 简单拼接排序

这会漏刷。

例如：

- 作者 A：`100, 99, 98`
- 作者 B：`97`

如果每个作者只取 1 条：

- 第一轮只拿到 `100, 97`
- 第二页 cursor 变成 `97`
- A 的 `99, 98` 会被永久跳过

## 10.1 正确做法：多路归并

必须做作者级有序流的 `k-way merge`。

思路：

1. 每个作者 outbox 都是一条按 `post_id DESC` 排序的有序流
2. 用最大堆维护“每个作者当前头部候选”
3. 每次弹出全局最大的 `post_id`
4. 某个作者当前 batch 用完后，再继续读取该作者下一批
5. 直到拿够：
   - `targetCount`
   - 以及用于 `hasMore/probe` 的额外候选

## 10.2 推荐的 Service 内部结构

建议引入作者流状态：

```go
type outboxAuthorStream struct {
    AuthorUserID int64
    LocalCursor  int64
    Buffer       []int64
    Exhausted    bool
}
```

以及一个 heap 节点：

```go
type outboxHeapItem struct {
    AuthorUserID int64
    PostID       int64
}
```

核心流程：

1. 对每个 `pullAuthor`，按 `global pull cursor` 拉第一批 `read_chunk_size`
2. 每个作者把头元素放入最大堆
3. 反复弹出最大 `post_id`
4. 从对应作者 buffer 中前进
5. 如果该作者 buffer 用完且未 exhausted：
   - 再拉下一批
6. 直到全局收集够本轮需要的 `post_id`

## 11. 与当前 cursor token 的兼容方案

当前 token 里已有：

- `InboxLastPostID`
- `PullLastPostID`
- `InboxPendingIDs`
- `PullPendingIDs`
- `RecentAuthorIDs`

### 设计结论

**outbox 方案第一版不修改 token 结构。**

也就是仍然保留：

- 一个全局 `PullLastPostID`
- 一组 `PullPendingIDs`

### 为什么可以不改 token

前提是：

- pull 候选收集必须做“正确的全局多路归并”

只要拿到的是：

- 在 `post_id < PullLastPostID` 条件下，全局真正最新的一段 pull 候选

那么当前 token 模型仍然成立：

- `PullLastPostID`
  - 继续表示 pull 侧的全局上界
- `PullPendingIDs`
  - 继续保存本页未消费、留给下页继续参与混排的 pull 候选

### 不需要 per-author cursor 的原因

因为当前不是“按作者分页”，而是“按全局 pull 流分页”。

只要 pull collector 保证：

- 这一轮拿到的是精确的全局最新候选

那 token 就不需要记录每个作者的本地游标。

## 12. Exposure 与 outbox 的组合方式

当前 `FeedService` 的 exposure 逻辑已经比较完整，推荐继续复用。

### 组合方式

outbox collector 先得到一批候选 `post_id` 后：

1. 如果开启 exposure：
   - 先调用 `FilterUnseenPostIDs(...)`
2. 再用过滤后的 `post_id` 回表 `ListByIDs(...)`
3. 如果曝光过滤、删帖过滤后数量仍不足：
   - 继续做 outbox merge 补拉

### 为什么先按 post_id 过滤 exposure

因为 outbox 只存：

- `post_id`

先做 ID 级过滤可以减少不必要的回表量。

## 13. Mix 策略如何兼容

这次 outbox 设计不建议修改混排策略本身。

原因是：

- 当前 mix 层只区分两个来源：
  - `inbox`
  - `pull`

而 outbox 本质上只是：

- 把 pull 来源从 `posts` 替换为更合适的作者级索引

所以：

- `feedMixSourcePull` 语义不变
- `mixPageForCursor(...)`
- `chooseFeedMixPick(...)`
- `push quota / pull reserve / author scatter / source scatter`

都可以保持不动

这是这套方案最重要的稳定点之一。

## 14. Refresh fallback 的处理

当前系统已有：

- refresh 首页空白时，回退到 latest pull

接入 outbox 后建议保持这个语义。

推荐做法：

1. 正常优先走 `inbox + outbox pull`
2. 如果是：
   - `refresh=1`
   - 首页
   - 结果为空
3. 则允许：
   - 优先尝试 DB latest fallback

这样可以继续兜住：

- outbox 短暂不一致
- 曝光过滤过重
- refresh 体验过空

## 15. Worker 设计调整

建议把 worker 能力扩成：

### `post_created`

1. `EnsureOutbox(authorID, postID)`
2. 按粉丝数决定 push/pull 模式
3. 小作者 fanout inbox
4. 失效粉丝 feed cache
5. 成功后 ACK

### `post_deleted`

1. `RemovePostFromOutbox(authorID, postID)`
2. 从粉丝 inbox 删除 `post_id`
3. 失效粉丝 feed cache
4. 成功后 ACK

### 为什么建议先处理 outbox

因为 outbox 是 pull-only 作者的主索引层。

先修 outbox，有两个好处：

1. 即使后续 inbox cleanup 或 cache invalidation 重试
   - pull 源已经尽量正确
2. `ZADD` / `ZREM` 本身更容易做成幂等式修复

## 16. Cache 设计是否变化

建议：

- **不修改当前 feed cache key 设计**

仍然保留：

- `feed:home:{user_id}:{cursor_or_last_post_id}:{limit}`

理由：

- API 输入没变
- token 结构暂时没变
- 返回结构没变

所以 cache 设计不需要因为 outbox 单独升级。

## 17. 一致性与降级策略总结

### 发帖

- DB 成功即可返回
- outbox 同步写失败不影响发帖成功
- worker 异步补写 outbox

### 删帖

- 主流程先标记 `status=deleted`
- worker 异步删 outbox / inbox
- 读路径只读 published，保证结果正确

### Outbox 读失败

- 若开启 `db_fallback_enabled`
  - 回退到当前 `posts` pull
- 否则走现有错误处理 / inbox-only 降级逻辑

### Follow / Unfollow

- 不改 outbox
- 继续靠：
  - cache invalidation
  - inbox author cleanup
  - allowlist 过滤

## 18. 推荐实现顺序

虽然本文档不保留 TODO 骨架，但从工程角度，推荐按下面顺序落地：

1. `FeedOutboxRepository`
2. 发帖同步写 outbox
3. worker `post_created/post_deleted` 维护 outbox
4. 批量读取作者粉丝数
5. `FeedService` 接入作者分组
6. outbox pull collector + heap merge
7. DB fallback
8. 补测试

## 19. 需要覆盖的测试点

至少应覆盖：

### Repository

1. `AddPostToOutbox`
2. `TrimOutbox`
3. `ListPostIDsByCursor`
4. `RemovePostFromOutbox`

### PostService

1. 发帖成功后 best-effort 写 outbox
2. outbox 写失败不影响发帖成功

### Worker

1. `post_created` 会补写 outbox
2. `post_deleted` 会移除 outbox

### FeedService

1. 多作者 outbox merge 顺序正确
2. 不会出现：
   - A: `100,99,98`
   - B: `97`
   - 第二页漏掉 `99/98`
3. `PullPendingIDs` 和 outbox 兼容
4. 自己的帖子仍能通过 pull 进入首页
5. outbox Redis 失败时会回退 DB pull
6. `pullAuthors` 数量超过阈值时会回退 DB pull
7. exposure + outbox 组合下仍能尽量补满页面

## 20. 最终设计结论

这套方案的核心是：

1. 所有作者写 outbox
2. 小作者继续写粉丝 inbox
3. 大作者 pull 优先从 outbox 来
4. `posts` 作为内容真身和 fallback 数据源继续保留
5. 不修改对外 API
6. 第一版不修改现有 cursor token 结构

它的优点是：

- 和当前系统连续演进，不推翻现有设计
- 大作者 pull 的数据源更像真正的 Feed 系统
- 一致性策略与当前 inbox / worker 思路保持一致
- mix、exposure、cache、token 都能尽量复用

这是当前项目里最稳妥、也最有工程含量的一版 outbox 方案。
