# 自动话题切换 — 任务列表

## Task 1: TopicSwitchDetector 核心实现
- [ ] 新建 `gui/im_topic_detector.go`
- [ ] 实现 `TopicSwitchDetector` 结构体
- [ ] 实现 BM25 快速判断逻辑（复用 corelib/bm25 包）
- [ ] 实现时间衰减系数计算
- [ ] 实现 `Detect(newMessage, userID string, memory *conversationMemory) TopicDecision`

## Task 2: LLM 确认调用
- [ ] 在 TopicSwitchDetector 中实现 `confirmWithLLM` 方法
- [ ] 构造极短 prompt（~50 tokens）
- [ ] 解析 same/new 响应
- [ ] 超时/错误时 fallback 为 TopicSame（保守策略）

## Task 3: 摘要存档 + 自动清理
- [ ] 在 IMMessageHandler 中实现 `archiveAndClear(userID)` 方法
- [ ] 从 conversationMemory 取最近几轮生成一句话摘要
- [ ] 存入 memoryStore（category: conversation_summary）
- [ ] 调用 memory.clear(userID)

## Task 4: 集成到消息处理流程
- [ ] 在 IMMessageHandler 中初始化 TopicSwitchDetector
- [ ] 在 HandleIMMessageWithProgressAndStream 中插入检测逻辑
- [ ] 位置：slash 命令之后、LLM 配置检查之后、正式处理之前
- [ ] 跳过 background 消息（IsBackground=true 不做话题检测）

## Task 5: conversationSession 增加 lastAccess 暴露
- [ ] 在 conversationMemory 上增加 `lastAccessTime(userID) time.Time` 方法
- [ ] 供 TopicSwitchDetector 计算时间衰减

## Task 6: 单元测试
- [ ] 测试 BM25 相似度判定（同话题 / 新话题 / 模糊地带）
- [ ] 测试时间衰减系数
- [ ] 测试 LLM fallback（超时返回 TopicSame）
- [ ] 测试 archiveAndClear 流程
