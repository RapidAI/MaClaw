# 八卦评分竞态失败 缺陷修复设计

## 概述

`GossipRateHandler` 和 `GossipCommentHandler`（带评分）中存在竞态条件：`HasRated` 从 `readDB` 读取，而 `CreateComment` 通过异步 `writeBatcher` 写入 `writeDB`。在 SQLite WAL 模式下，batcher 队列中尚未刷盘的写入对 `readDB` 不可见，导致快速双击时重复评分请求均通过检查。修复策略是让评分操作绕过 batcher，在 `writeDB` 上使用直接事务完成"检查+插入+更新分数"的原子操作，同时添加数据库唯一约束作为最终防线，并传播 `UpdatePostScore` 错误。

## 术语表

- **Bug_Condition (C)**：触发缺陷的条件——同一 `(post_id, machine_id)` 的评分请求在 batcher 刷盘前到达，导致 `HasRated` 检查失效
- **Property (P)**：期望行为——同一 `(post_id, machine_id)` 只能有一条 `rating > 0` 的评分记录，第二次应返回 `409 ALREADY_RATED`
- **Preservation**：不变行为——纯评论（`rating=0`）、不同用户评分、发帖、锁定帖子拒绝等现有行为不受影响
- **writeBatcher**：`hubcenter/internal/store/sqlite/batcher.go` 中的异步批量写入器，将多条写操作攒批后在单个事务中刷盘
- **execWrite**：`repositories_stub.go` 中的辅助函数，当 `batch != nil` 时委托给 batcher，否则直接执行
- **gossipRepo**：`repositories_stub.go` 中的结构体，持有 `db`（写连接）、`readDB`（读连接）和 `batch`（writeBatcher）

## 缺陷详情

### Bug Condition

缺陷在以下条件下触发：同一用户（`machine_id`）对同一帖子（`post_id`）快速发起多次评分请求。`HasRated` 在 `readDB` 上查询 `gossip_comments` 表，而前一次 `CreateComment` 的写入仍在 `writeBatcher` 队列中等待刷盘，`readDB` 在 WAL 模式下看不到该写入，因此两次请求均通过重复检查。

**形式化规约：**
```
FUNCTION isBugCondition(input)
  INPUT: input of type RateRequest{postID, machineID, rating, timestamp}
  OUTPUT: boolean

  LET pendingWrites = writeBatcher.pendingJobs()
  LET hasRatedInDB = SELECT COUNT(*) FROM gossip_comments
                     WHERE post_id = input.postID
                       AND machine_id = input.machineID
                       AND rating > 0
                     USING readDB

  RETURN input.rating >= 1
         AND input.rating <= 5
         AND hasRatedInDB == 0
         AND EXISTS w IN pendingWrites WHERE
             w.postID == input.postID
             AND w.machineID == input.machineID
             AND w.rating > 0
END FUNCTION
```

### 示例

- 用户 `machine-A` 对帖子 `post-1` 评分 5 星，请求 R1 进入 handler，`HasRated` 返回 false，`CreateComment` 将 INSERT 加入 batcher 队列。在 batcher 刷盘前，请求 R2 到达，`HasRated` 仍返回 false（readDB 看不到队列中的写入），R2 也通过检查。batcher 刷盘时两条 INSERT 在同一事务中执行，若无唯一约束则两条都成功（数据重复），若有约束则第二条失败导致整个事务回滚（两条都丢失）。
- 用户 `machine-B` 通过 `/api/gossip/comment` 端点提交带 `rating=3` 的评论，同样存在上述竞态。
- `CreateComment` 成功但 `UpdatePostScore` 失败时，`_ =` 静默忽略错误，handler 返回 200 OK，但帖子的 `score`/`votes` 字段未更新。

## 期望行为

### Preservation Requirements

**不变行为：**
- 不同用户（不同 `machine_id`）对同一帖子评分必须继续正常工作
- 纯评论（`rating = 0`）必须继续正常创建，不受评分防重逻辑影响
- 首次评分（无重复）必须继续正常创建评分记录、更新帖子分数并返回成功
- 锁定帖子必须继续拒绝评分请求并返回 `403 LOCKED`
- 评分值不在 1-5 范围内必须继续返回 `400 BAD_REQUEST`
- 发帖、纯评论等非评分类写入必须继续通过 batcher 批量执行

**范围：**
所有不涉及评分竞态条件的输入不应受此修复影响，包括：
- 纯评论（`rating = 0`）的创建
- 不同 `machine_id` 的评分
- 帖子的发布、浏览、删除、锁定
- 管理员操作

## 假设根因

基于代码分析，最可能的问题是：

1. **读写分离导致的可见性间隙**：`HasRated` 使用 `readDB` 查询，而 `CreateComment` 通过 `writeBatcher` 异步写入 `writeDB`（`db` 字段）。在 SQLite WAL 模式下，`readDB` 连接只能看到已提交的写入，batcher 队列中的 pending jobs 对 `readDB` 不可见。这是竞态的根本原因。

2. **batcher 事务的级联失败**：`flush()` 方法在单个事务中执行整批写入，任一条失败则 `tx.Rollback()` 回滚全部。如果两条重复评分进入同一批次且触发约束冲突，同批次中其他不相关的写入也会被回滚。

3. **缺少数据库层面的唯一约束**：`gossip_comments` 表上没有 `(post_id, machine_id)` 且 `rating > 0` 的唯一约束，数据库无法作为最终防线阻止重复评分。

4. **UpdatePostScore 错误被静默忽略**：`_ = gossip.UpdatePostScore(...)` 丢弃了错误，导致评分记录存在但帖子分数未更新。

## 正确性属性

Property 1: Bug Condition - 评分原子性防重

_For any_ 评分请求，其中 `rating >= 1` 且同一 `(post_id, machine_id)` 已存在 `rating > 0` 的评分记录（无论是通过直接事务还是之前的请求写入），修复后的 `RateComment` 方法 SHALL 返回错误（或 handler 返回 `409 ALREADY_RATED`），且不会插入重复的评分记录。

**Validates: Requirements 2.1, 2.2**

Property 2: Preservation - 非竞态输入行为不变

_For any_ 输入，其中 bug condition 不成立（纯评论 `rating=0`、不同 `machine_id` 的评分、首次评分、发帖等），修复后的代码 SHALL 产生与原始代码相同的结果，保留所有现有功能。

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6**

## 修复实现

### 所需变更

假设根因分析正确：

**文件**: `hubcenter/internal/store/store.go`

**接口**: `GossipRepository`

**具体变更**:
1. **新增 `RateComment` 方法**：在 `GossipRepository` 接口中添加 `RateComment(ctx context.Context, comment *GossipComment) error` 方法，该方法将"检查是否已评分 + 插入评分记录 + 更新帖子分数"三步操作封装在同一个写连接事务中。

---

**文件**: `hubcenter/internal/store/sqlite/repositories_stub.go`

**结构体**: `gossipRepo`

**具体变更**:
2. **实现 `RateComment` 方法**：绕过 batcher，直接在 `r.db` 上开启事务：
   - `BEGIN IMMEDIATE` 获取写锁
   - 在事务内执行 `SELECT COUNT(*) FROM gossip_comments WHERE post_id = ? AND machine_id = ? AND rating > 0` 检查是否已评分
   - 若已评分，回滚事务并返回自定义 `ErrAlreadyRated` 错误
   - 若未评分，执行 `INSERT INTO gossip_comments ...`
   - 执行 `UPDATE gossip_posts SET score = ..., votes = ... WHERE id = ?` 更新帖子分数
   - `COMMIT` 提交事务
   - 这确保了检查和写入在同一个写连接事务中完成，消除了竞态窗口

---

**文件**: `hubcenter/internal/store/sqlite/migrations.go`

**函数**: `RunMigrations`

**具体变更**:
3. **添加唯一约束**：在 migrations 末尾添加 `CREATE UNIQUE INDEX IF NOT EXISTS idx_gossip_comments_unique_rating ON gossip_comments(post_id, machine_id) WHERE rating > 0`（SQLite 支持 partial unique index），作为数据库层面的最终防线。

---

**文件**: `hubcenter/internal/httpapi/gossip_handler.go`

**函数**: `GossipRateHandler`, `GossipCommentHandler`

**具体变更**:
4. **GossipRateHandler 使用 `RateComment`**：替换原来的 `HasRated` + `CreateComment` + `_ = UpdatePostScore` 调用链，改为调用 `gossip.RateComment(ctx, comment)`。根据返回的错误类型判断：若为 `ErrAlreadyRated` 则返回 `409 ALREADY_RATED`，若为其他错误则返回 `500`。

5. **GossipCommentHandler 中 `rating > 0` 的分支也使用 `RateComment`**：当 `req.Rating > 0` 时，调用 `gossip.RateComment` 而非 `CreateComment`，确保通过评论端点提交的评分也具有原子性。

6. **移除 `_ = gossip.UpdatePostScore` 调用**：因为 `RateComment` 内部已包含 `UpdatePostScore`，且错误会通过事务传播。

## 测试策略

### 验证方法

测试策略分两阶段：首先在未修复代码上发现反例以确认根因，然后验证修复后的代码正确工作且保留现有行为。

### 探索性 Bug Condition 检查

**目标**：在实施修复前，发现能复现缺陷的反例，确认或否定根因分析。若否定，需重新假设。

**测试计划**：编写测试模拟并发评分请求，在未修复代码上运行以观察失败模式。

**测试用例**:
1. **并发双击评分测试**：同一 `machine_id` 对同一帖子同时发起两次评分请求（在未修复代码上将导致重复记录或批量回滚）
2. **评论端点带评分并发测试**：通过 `/api/gossip/comment` 端点同时提交两次带 `rating > 0` 的请求（在未修复代码上将导致相同问题）
3. **UpdatePostScore 失败测试**：模拟 `UpdatePostScore` 返回错误，验证 handler 是否静默忽略（在未修复代码上将返回 200 OK）

**预期反例**:
- 两次并发评分请求均返回 200 OK，数据库中出现两条评分记录
- 或 batcher 刷盘时事务回滚，两次请求均失败
- `UpdatePostScore` 失败时 handler 仍返回 200 OK

### Fix Checking

**目标**：验证对于所有满足 bug condition 的输入，修复后的函数产生期望行为。

**伪代码：**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := RateComment_fixed(input)
  ASSERT result == ErrAlreadyRated
  ASSERT countRatings(input.postID, input.machineID) == 1
END FOR
```

### Preservation Checking

**目标**：验证对于所有不满足 bug condition 的输入，修复后的函数产生与原始函数相同的结果。

**伪代码：**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT originalBehavior(input) == fixedBehavior(input)
END FOR
```

**测试方法**：推荐使用 property-based testing 进行 preservation checking，因为：
- 它能自动生成大量测试用例覆盖输入域
- 它能捕获手动单元测试可能遗漏的边界情况
- 它能提供强保证：所有非缺陷输入的行为不变

**测试计划**：先在未修复代码上观察非缺陷输入的行为，然后编写 property-based test 捕获该行为。

**测试用例**:
1. **不同用户评分保留**：观察不同 `machine_id` 对同一帖子评分在未修复代码上正常工作，编写测试验证修复后仍然正常
2. **纯评论保留**：观察 `rating=0` 的评论在未修复代码上正常创建，编写测试验证修复后仍然正常
3. **首次评分保留**：观察首次评分在未修复代码上正常工作，编写测试验证修复后仍然正常
4. **锁定帖子保留**：观察锁定帖子拒绝评分在未修复代码上正常工作，编写测试验证修复后仍然正常

### 单元测试

- 测试 `RateComment` 方法的原子性：首次评分成功，重复评分返回 `ErrAlreadyRated`
- 测试 `RateComment` 方法在 `UpdatePostScore` 失败时回滚整个事务
- 测试唯一约束：直接 INSERT 重复评分记录被数据库拒绝
- 测试 `GossipRateHandler` 对 `ErrAlreadyRated` 返回 409
- 测试 `GossipCommentHandler` 中 `rating > 0` 时使用 `RateComment`

### Property-Based Tests

- 生成随机的 `(post_id, machine_id, rating)` 组合，验证同一 `(post_id, machine_id)` 只能有一条 `rating > 0` 的记录
- 生成随机的不同 `machine_id` 集合，验证每个都能成功评分且帖子分数正确更新
- 生成随机的 `rating=0` 评论，验证不受评分防重逻辑影响

### 集成测试

- 测试完整的评分流程：发帖 → 评分 → 验证分数更新 → 重复评分返回 409
- 测试并发评分场景：多个 goroutine 同时对同一帖子评分，验证最终只有一条评分记录
- 测试混合操作：评分 + 纯评论 + 不同用户评分，验证互不干扰
