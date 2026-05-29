# 记忆管理缺陷改进计划

> **来源**：2026-05-29 系统性审计
> **实施状态**：第一批已完成（缺陷 #1、#2、#3）

## 识别出的缺陷（按优先级排序）

### 缺陷 1 (P0): Frozen Snapshot 过度缓存——会话内记忆写入对 LLM 不可见

**现象**：用户说"记住我叫张三"→ LLM 保存成功 → 用户说"我叫什么"→ LLM 从 system prompt 的 frozen snapshot 中找不到"张三"。

**根因**：`appendMemorySection` 的静态部分（UserFactSummary）被缓存为 frozen snapshot，整个会话期间复用。`RefreshMemorySnapshot` 只在 `clearPerUserSessionState`（/new、话题切换）时调用。会话内的 `memory(action=save)` 不触发刷新。

**修复**：memory tool 的 save action 成功后，调用 `RefreshMemorySnapshot(userID)` 使 frozen snapshot 失效。下一条消息会重新生成包含新 user_fact 的 snapshot。

**修改文件**：
- `gui/im_tools_misc.go`：`toolMemory` 的 save 分支成功后调用 `h.RefreshMemorySnapshot(userID)`

**风险**：低。只是让 snapshot 提前失效，下次消息重新生成。不影响 KV cache 前缀稳定性（snapshot 在同一条消息内不变，只在下一条消息时重新生成）。

---

### 缺陷 2 (P0): Proactive Recall 的 category 过滤在 tool recall 路径误杀 user_fact

**现象**：LLM 调用 `memory(action=recall, query="个人信息")` 不指定 category 时，`user_fact` 被过滤，返回空。

**根因**：#89.1 修复将 `user_fact`/`self_identity`/`session_checkpoint`/`conversation_summary` 的过滤从 `appendProactiveRecall` 下沉到 `RecallDynamic` 内部（`category==""` 时统一过滤）。但 `toolMemory` 的 recall action 也调用 `RecallDynamic(query, "", projectPath)`，category 为空，user_fact 被误杀。

**根因分析**：过滤策略被硬编码在数据层（`recallDynamicEntryAllowed`）而非调用层。数据层不应该知道"谁在调用我"。过滤策略应该由调用方决定。

**机制性修复**：将 category 排除列表从硬编码提升为参数化。

- 新增 `recallFilterOptions` 结构体，包含 `excludeWhenNoCategory []Category`
- 新增 `recallDynamicCoreWithOptions`——统一的召回引擎，排除策略由调用方通过 options 传入
- `recallDynamicEntryAllowed` 重写为 `recallDynamicEntryAllowedWithExclusions`——接受排除列表参数
- 声明两个排除策略常量：
  - `proactiveRecallExcludeCategories`：排除 user_fact/self_identity/session_checkpoint/conversation_summary（proactive recall 使用）
  - `toolRecallExcludeCategories`：只排除 session_checkpoint/conversation_summary（tool recall 使用）
- `RecallDynamic` 使用 proactive 策略，`RecallDynamicForTool` 使用 tool 策略
- `RecallByMode`（tool 入口）改用 `RecallDynamicForTool`

**设计原则**：排除策略属于调用方，不属于数据层。数据层提供参数化的过滤机制，调用方声明自己的策略。新增调用方只需定义自己的 `[]Category` 排除列表。

**修改文件**：
- `corelib/memory/store.go`：`recallFilterOptions` + `recallDynamicCoreWithOptions` + `recallDynamicEntryAllowedWithExclusions` + 两个策略常量
- `corelib/memory/tool_service.go`：`RecallByMode` 改用 `RecallDynamicForTool`

---

### 缺陷 3 (P1): OnlineExtractor 与 KnowledgeExtractor 互斥的 NOOP 时间窗口

**现象**：OnlineExtractor 连续 60 分钟只返回 NOOP 后，KnowledgeExtractor 不再被抑制，两者同时提取产生重复。

**根因**：`HasRecentSuccess` 只在 ADD/UPDATE/DELETE 时更新 `lastSuccess`。NOOP 不更新。

**修复**：新增 `lastActivity` 字段，在任何非空 extraction（包括全 NOOP）时更新。`HasRecentSuccess` 改为 `HasRecentActivity`，检查 `lastActivity`。

**修改文件**：
- `corelib/memory/online_extractor.go`：新增 `lastActivity` + `HasRecentActivity`
- `corelib/memory/knowledge_extractor.go`：从 `HasRecentSuccess` 改为 `HasRecentActivity`

---

### 缺陷 4 (P1): CompactForm 生成延迟——新 entry 在 6 小时内使用完整内容注入

**现象**：新保存的 entry 在 CompactForm 生成前被 proactive recall 注入时使用完整 Content（800+ 字符），浪费 token 预算。

**根因**：CompactForm 只在 Pipeline 的 `backfillCompactForms`（每 6 小时）中生成。

**修复**：`SaveWithContext` 成功后，如果 content > 300 rune 且 LLM 可用，异步生成 CompactForm（goroutine，不阻塞 Save 返回）。

**修改文件**：
- `corelib/memory/store.go`：`SaveWithContext` 末尾新增异步 CompactForm 生成

---

### 缺陷 5 (P2): 子串去重扫描范围不一致

**现象**：Save 路径只扫描最近 50 条，可能漏掉第 51-2000 条中的重复。

**根因**：性能考虑限制了扫描范围，但 2000 条的线性扫描实际只需 <5ms。

**修复**：将扫描范围从 50 条扩大到 200 条（仍远小于 2000，但覆盖了绝大多数近期重复）。

---

### 缺陷 6 (P2): TemporalTree 与 entry eviction 不同步

**现象**：entry 被 GC evict 后，TemporalTree 中的 node ID 仍存在，consolidation 基于不完整数据。

**修复**：`evictToArchive` 后同步从 TemporalTree 中移除对应 node。

---

## 实施计划

| 缺陷 | 优先级 | 预估工作量 | 实施顺序 |
|------|--------|-----------|---------|
| #1 Frozen Snapshot | P0 | 10 行 | 第一批 |
| #2 Tool Recall 误杀 | P0 | 30 行 | 第一批 |
| #3 互斥时间窗口 | P1 | 15 行 | 第一批 |
| #4 CompactForm 延迟 | P1 | 40 行 | 第二批 |
| #5 子串去重范围 | P2 | 5 行 | 第二批 |
| #6 TMT eviction | P2 | 20 行 | 第二批 |

第一批（本次实施）：缺陷 #1、#2、#3
