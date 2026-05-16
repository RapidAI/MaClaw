# 八卦评分竞态失败 - 实施任务

## Task 1: 添加数据库唯一约束迁移
- [x] 1.1 在 `hubcenter/internal/store/sqlite/migrations.go` 的 `RunMigrations` 末尾添加 partial unique index：`CREATE UNIQUE INDEX IF NOT EXISTS idx_gossip_comments_unique_rating ON gossip_comments(post_id, machine_id) WHERE rating > 0`
- [x] 1.2 验证迁移在已有数据的数据库上可重复执行（`IF NOT EXISTS`）

**Requirements**: Design §修复实现 变更3

## Task 2: 定义 ErrAlreadyRated 错误 + GossipRepository 接口新增 RateComment
- [x] 2.1 在 `hubcenter/internal/store/store.go` 中定义 `var ErrAlreadyRated = errors.New("already rated")` 哨兵错误
- [x] 2.2 在 `GossipRepository` 接口中添加 `RateComment(ctx context.Context, comment *GossipComment) error` 方法

**Requirements**: Design §修复实现 变更1

## Task 3: 实现 gossipRepo.RateComment 原子事务方法
- [x] 3.1 在 `hubcenter/internal/store/sqlite/repositories_stub.go` 的 `gossipRepo` 上实现 `RateComment` 方法
- [x] 3.2 绕过 batcher，直接在 `r.db` 上开启 `BEGIN IMMEDIATE` 事务
- [x] 3.3 事务内 SELECT 检查是否已评分（`rating > 0`），若已评分则回滚并返回 `store.ErrAlreadyRated`
- [x] 3.4 若未评分，INSERT 评分记录
- [x] 3.5 在同一事务内 UPDATE `gossip_posts` 的 `score` 和 `votes` 字段
- [x] 3.6 COMMIT 提交事务，任一步骤失败则回滚

**Requirements**: Design §修复实现 变更2

## Task 4: 修改 GossipRateHandler 和 GossipCommentHandler 使用 RateComment
- [x] 4.1 在 `hubcenter/internal/httpapi/gossip_handler.go` 的 `GossipRateHandler` 中，替换 `HasRated` + `CreateComment` + `_ = UpdatePostScore` 调用链为 `gossip.RateComment(ctx, comment)`
- [x] 4.2 根据返回错误类型判断：`errors.Is(err, store.ErrAlreadyRated)` → 返回 409 ALREADY_RATED；其他错误 → 返回 500
- [x] 4.3 在 `GossipCommentHandler` 中，当 `req.Rating > 0` 时同样使用 `gossip.RateComment` 替代原有逻辑
- [x] 4.4 移除所有 `_ = gossip.UpdatePostScore` 的静默忽略调用

**Requirements**: Design §修复实现 变更4, 5, 6

## Task 5: 编写单元测试验证修复
- [x] 5.1 测试 `RateComment` 首次评分成功
- [x] 5.2 测试 `RateComment` 重复评分返回 `ErrAlreadyRated`
- [x] 5.3 测试 handler 对 `ErrAlreadyRated` 返回 409 状态码
- [x] 5.4 测试纯评论（`rating=0`）不受影响，仍走 `CreateComment`
- [x] 5.5 测试不同 `machine_id` 对同一帖子评分均成功
- [x] 5.6 编写并发测试：多个 goroutine 同时对同一 `(post_id, machine_id)` 评分，验证最终只有一条评分记录

**Requirements**: Design §测试策略 - Fix Checking, Preservation Checking

## Task 6: 编译验证 + 运行测试
- [x] 6.1 `go build ./hubcenter/...` 编译通过
- [x] 6.2 `go test ./hubcenter/...` 所有测试通过
- [x] 6.3 确认现有 `gossip_handler_test.go` 中的测试不受影响

**Requirements**: Design §测试策略 - Preservation Checking
