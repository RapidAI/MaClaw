# 记忆召回增强 — 任务列表

## Task 1: Query Expand 模块
- [x] 新建 `corelib/memory/query_expand.go`
  - `ExpandResult` 结构体（Entities + QueryTokens）
  - `ExpandQuery(userMessage string) ExpandResult`
  - 正则模式：引号内容、IP、域名、文件路径、数字+名词、英文专有名词、英文技术词、中文复合名词
  - 停用词过滤表（中文动词/助词/代词）+ splitOnChineseStopwords
  - 去重，Entities ≤5，QueryTokens ≤20
- [x] 新建 `corelib/memory/query_expand_test.go`
  - 20 个测试用例全部通过，覆盖各类实体提取场景

## Task 2: Tag 交叉匹配
- [x] 修改 `corelib/memory/store.go`
  - 新增 `tagCrossScore(entry Entry, queryTokens []string) float64`
  - 修改 `rrfFuseScores` 签名增加 `queryTokens []string`
  - 三路 RRF 融合：BM25 + Vec + Tag（α=0.8）
  - queryTokens 为 nil 时行为不变（向后兼容）
- [x] 更新 `RecallForProject` 和 `RecallDynamic` 中的 rrfFuseScores 调用

## Task 3: Multi-Query BM25
- [x] 修改 `corelib/memory/store.go`
  - 新增 `multiQueryBM25(userMessage string, entities []string) map[string]float64`
  - 修改 `RecallForProject` 使用 multiQueryBM25 替代 s.bm25.score

## Task 4: 动态 Token 预算与类型配额
- [x] 修改 `corelib/memory/store.go`
  - 新增 `dynamicBudget(activeCount int) (maxTokens, maxEntries int)`
  - 新增 `activeCountLocked() int`
  - 修改 `RecallForProject` 使用动态预算
  - 实现 user_fact 60% 上限 + 其他类型 40% 保底

## Task 5: Proactive Recall 注入
- [x] 修改 `gui/im_system_prompt.go`
  - `appendMemorySection` 增加 `userMessage ...string` 可变参数
  - 调用 RecallForProject 获取相关记忆
  - 过滤 user_fact/self_identity
  - 格式化注入 "相关记忆（自动召回）" section，最多 8 条，每条截断 200 字符
- [x] 修改 `buildSystemPromptBase` 接受 `userMessage ...string` 并传递
- [x] 修改 `buildSystemPromptWithMemory` 传递 userMessage
- [x] 更新 `im_message_handler_proactive_memory_test.go` 适配新提示文本

## Task 6: LLM Reranker 可选增强
- [x] 修改 `corelib/memory/store.go`
  - LLMRelevanceFilter 增加 `IsAvailable() bool`
  - 新增 `RecallSmart` 方法（RecallForProject + 可选 LLM rerank）

## Task 7: 回归测试与集成验证
- [x] 全部 corelib/memory 测试通过（含 query_expand 20 个新测试）
- [x] 全部 gui SystemPrompt/Memory 测试通过
- [x] corelib/...、gui/...、tui/... 编译通过
