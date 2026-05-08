# Feed V2 Config Guide

本文档说明当前 `feed-flow-system` 中 Feed V2 相关配置项的作用、默认值和调优方向。

目标不是做“大而全”的推荐系统，而是让这套 feed 系统具备：

- Push / Pull 混合分发
- 读路径混排
- 基础曝光去重
- 可解释、可调优

## 配置入口

当前配置位于：

- `configs/config.yaml`
- `configs/config.example.yaml`

核心配置路径：

- `feed.hybrid`
- `feed.hybrid.mix`
- `feed.inbox`
- `feed.exposure`

## 1. Push / Pull 分流配置

### `feed.hybrid.push_follower_threshold`

示例：

```yaml
feed:
  hybrid:
    push_follower_threshold: 100
```

含义：

- 作者粉丝数 `<= threshold`：走 `push_and_pull`
- 作者粉丝数 `> threshold`：走 `pull_only`

当前代码入口：

- `internal/service/feed_hybrid_policy.go`
- `internal/service/feed_invalidation_worker.go`

设计意图：

- 小号粉丝少，push 到粉丝 inbox 成本可控，能提升读性能
- 大号粉丝多，强推会造成 fanout 成本高，所以只保留 pull

调大后的效果：

- 更多作者会进入 push 分发
- inbox 覆盖率更高
- worker fanout 压力更大

调小后的效果：

- 更多作者退回 pull_only
- 写扩散成本更低
- 读路径更依赖 pull 查询

特殊值：

- `0` 表示所有作者都走 `pull_only`
- 这是当前系统的全局回退开关之一

## 2. 读路径混排配置

### `feed.hybrid.mix.push_ratio_numerator`
### `feed.hybrid.mix.push_ratio_denominator`

示例：

```yaml
feed:
  hybrid:
    mix:
      push_ratio_numerator: 2
      push_ratio_denominator: 3
```

含义：

- 控制一页里 push 候选的理论配额
- 当前默认 `2/3`，表示页面大约 2/3 的核心坑位优先留给 inbox 来源

当前代码入口：

- `internal/service/feed_mix_strategy.go`
- `resolvePushQuota(...)`

设计意图：

- inbox 本质上来自“小号 push 内容”
- pull 本质上兜底“大号内容 + 其他未入 inbox 内容”
- 这个比例决定“push 来源内容”在一页里的上限倾向

注意：

- 这不是绝对比例
- 最终结果还会受以下因素共同影响：
  - pull 是否有候选
  - `min_pull_items`
  - 同作者打散
  - 帖子 ID 新鲜度比较

调大后的效果：

- 页面更偏向 inbox
- 小号内容更容易占据前排

调小后的效果：

- 页面更偏向 pull
- 大号 / pull-only 内容更容易前置

### `feed.hybrid.mix.min_pull_items`

示例：

```yaml
feed:
  hybrid:
    mix:
      min_pull_items: 1
```

含义：

- 当 pull 候选存在时，每页至少保留多少个 pull 坑位

当前代码入口：

- `internal/service/feed_mix_strategy.go`
- `resolveMinPullItems(...)`
- `chooseFeedMixPick(...)`

设计意图：

- 防止 inbox 太满时，页面被 push 内容完全占据
- 给 pull-only 作者留最基本曝光入口

调大后的效果：

- pull 内容保底更多
- 大号内容更稳定出现
- inbox 的主导性降低

调小后的效果：

- 页面更容易被 inbox 占满
- pull-only 作者更依赖其他策略才能露出

特殊说明：

- 当前实现里：
  - `min_pull_items: 0` 表示关闭 pull 保底
  - 未配置该项时，仍走默认值 `1`

### `feed.hybrid.mix.max_consecutive_author`

示例：

```yaml
feed:
  hybrid:
    mix:
      max_consecutive_author: 2
```

含义：

- 当存在其他作者候选时，尽量避免同一作者连续超过 N 条

当前代码入口：

- `internal/service/feed_mix_strategy.go`
- `shouldAvoidSameAuthor(...)`
- `nextFeedMixPick(...)`

设计意图：

- 做基础“作者打散”
- 避免同作者连续刷屏影响观感

调大后的效果：

- 打散变弱
- 更偏向单纯按新鲜度排列

调小后的效果：

- 打散更强
- 同作者内容更容易被延后

注意：

- 这是“尽量避免”，不是强约束
- 如果没有其他作者候选，系统仍会继续返回该作者内容

### `feed.hybrid.mix.author_cooldown_window`

示例：

```yaml
feed:
  hybrid:
    mix:
      author_cooldown_window: 2
```

含义：

- 在最近 K 条已选内容中，如果某作者已经出现过，并且存在可替代作者，则优先避开该作者

当前代码入口：

- `internal/service/feed_mix_strategy.go`
- `nextFeedMixPick(...)`
- `isAuthorInRecentHistory(...)`

设计意图：

- `max_consecutive_author` 只管“连续刷屏”
- `author_cooldown_window` 管“短窗口重复出现”
- 两者叠加后，能减少“AA_B_A”这类短距离重复

调大后的效果：

- 页面作者多样性更强
- 但可能牺牲部分最新内容的前排优先级

调小后的效果：

- 更偏向时间新鲜度
- 作者重复更容易出现

特殊说明：

- `0` 表示关闭窗口冷却，只保留连续打散

### `feed.hybrid.mix.max_consecutive_source`

示例：

```yaml
feed:
  hybrid:
    mix:
      max_consecutive_source: 2
```

含义：

- 当 inbox 与 pull 都有可选内容时，尽量避免同一来源连续超过 N 条
- 来源指的是：`inbox` 或 `pull`

当前代码入口：

- `internal/service/feed_mix_strategy.go`
- `chooseFeedMixPick(...)`
- `shouldAvoidSameSource(...)`

设计意图：

- `push_ratio` 和 `min_pull_items` 解决的是“总量比例/保底”
- `max_consecutive_source` 解决的是“局部节奏”
- 避免出现一段连续全是 inbox 或全是 pull 的观感

调大后的效果：

- 源级打散更弱，排序更偏向原始新鲜度

调小后的效果：

- 源级打散更强，inbox/pull 交替更明显

特殊说明：

- 当前实现里：
  - `max_consecutive_source: 0` 表示关闭来源连续打散
  - 未配置该项时，走默认值 `2`

## 3. Inbox 配置

### `feed.inbox.enabled`

示例：

```yaml
feed:
  inbox:
    enabled: true
```

含义：

- 是否开启 inbox 读路径

调成 `false` 后：

- 读路径直接退回 pull 能力
- 不再读 Redis inbox

### `feed.inbox.max_items`

示例：

```yaml
feed:
  inbox:
    max_items: 1000
```

含义：

- 单用户 inbox 最多保留多少个 post_id

当前代码入口：

- `internal/service/feed_inbox_fanout.go`
- `internal/repository/feed_inbox_repository.go`

设计意图：

- 控制单用户 inbox 的 Redis 占用
- 防止 fanout 长期无限增长

调大后的效果：

- inbox 可覆盖更长时间窗口
- Redis 存储成本更高

调小后的效果：

- inbox 更短
- 更容易回退依赖 pull

### `feed.inbox.batch_size`
### `feed.inbox.workers`

示例：

```yaml
feed:
  inbox:
    batch_size: 200
    workers: 8
```

含义：

- `batch_size`：一次 pipeline fanout 写多少个 follower inbox
- `workers`：worker 内部并发处理多少个 batch

当前代码入口：

- `internal/service/feed_inbox_fanout.go`
- `cmd/worker/main.go`

设计意图：

- 控制 push fanout 的吞吐与单次批量大小
- 避免单个发帖事件要么太碎、要么单批过大

调大后的效果：

- 写入吞吐更高
- 但单次 Redis pipeline 更重，失败时影响面更大

调小后的效果：

- 单批更轻、更容易观察问题
- 但总 RTT 和调度成本更高

## 4. Exposure 去重配置

### `feed.exposure.enabled`

示例：

```yaml
feed:
  exposure:
    enabled: true
```

含义：

- 是否开启曝光去重

关闭后效果：

- Feed 读路径不再过滤近期已曝光帖子
- 返回到“无曝光窗口”的行为

### `feed.exposure.window_hours`

示例：

```yaml
feed:
  exposure:
    window_hours: 24
```

含义：

- 一条帖子在用户的曝光窗口中保留多久
- 这段时间内再次拉 feed，会尽量过滤掉同一 `post_id`

当前代码入口：

- `internal/service/feed_exposure.go`
- `internal/repository/feed_exposure_repository.go`

设计意图：

- 减少刷新时重复刷到同一帖子
- 提升“新鲜感”

调大后的效果：

- 重复内容更少
- 但更容易导致“近期内容被过滤太狠”，需要更积极回填

调小后的效果：

- 去重窗口更短
- 用户更容易重复看到帖子

### `feed.exposure.key_ttl_hours`

示例：

```yaml
feed:
  exposure:
    key_ttl_hours: 48
```

含义：

- Redis `feed:exposure:{user_id}` 这个 key 的过期时间

和 `window_hours` 的区别：

- `window_hours`：业务上“多长时间算已曝光”
- `key_ttl_hours`：Redis 里整把 key 多久清掉

设计意图：

- 给冷用户的曝光记录自动回收
- 避免曝光 key 永久堆积

通常建议：

- `key_ttl_hours >= window_hours`

### `feed.exposure.batch_multiplier`

示例：

```yaml
feed:
  exposure:
    batch_multiplier: 3
```

含义：

- 当开启 exposure 去重时，读路径为了补齐 limit，会超拉更多候选
- 超拉批次大小约为：`limit * batch_multiplier`

当前代码入口：

- `internal/service/feed_exposure.go`
- `resolveFeedExposureBatchLimit(...)`

设计意图：

- 因为很多候选可能被 exposure 过滤掉
- 不超拉的话，返回结果很容易填不满

调大后的效果：

- 更容易补满一页
- 但 DB / Redis 读放大更明显

调小后的效果：

- 查询成本更低
- 但返回页更容易不满

## 5. 一套推荐的理解方式

可以这样理解当前系统：

1. 写路径按作者粉丝量做 push/pull 分流
2. 小号发帖后写入粉丝 inbox，大号只保留 pull
3. 读路径同时读取 inbox 候选和 pull 候选
4. 通过混排策略控制：
   - push 内容比例
   - pull 保底
   - 同作者打散
5. 再叠加 exposure 窗口，减少刷新重复
6. 如果 Redis 能力异常，可以降级回 pull 读路径

## 6. 当前已知限制

当前实现仍有几个“刻意保留给后续增强”的点：

1. 混排参数是全局配置，不是按用户分群配置
2. exposure 只按 `post_id` 做最近曝光去重，没有更复杂的召回打散
3. push fanout 仍是朴素写法，后续应做 pipeline / 幂等优化
4. pull 数据源当前仍以 `posts` 为主，作者级 `outbox` 设计见：
   - `docs/feed-outbox-design.md`

## 7. 下一步建议

如果继续推进 Feed V2，建议按这个顺序：

1. 增加更强打散策略
2. 做 push fanout pipeline / 幂等优化
3. 补一版架构图和时序图
