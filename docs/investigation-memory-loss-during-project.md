# 调查报告：MacLaw 项目进行中失忆问题（修复 #56）

## 现象

用户在编程项目进行过程中（FileAPITester 已完成，RegistryAPITester 需求已确认），发送"开始开发吧"后，maclaw 只知道 FileAPITester 完成了，不知道 RegistryAPITester 的需求文档。用户说"你再查查看一下记忆"后，maclaw 通过 memory recall 才找回两个项目的完整状态。

## 根因分析

### 核心问题：对话历史的数据模型是扁平的

对话历史 `[]ConversationEntry` 把所有信息扁平存储——用户的任务请求、LLM 的规划文档、工具调用细节、工具返回结果，全部混在一个列表里。然后用一个固定数字 `MaxConversationTurns = 40` 做 FIFO 截断。

编程任务中，一个 agent loop 迭代产生 3-5 条 entries（assistant + 多个 tool results），35 轮迭代就是 100+ 条 entries。FIFO 截断到 40 条后，只剩最近 5-10 轮的工具执行细节，早期的任务请求和规划文档被永久丢弃。

这不是"哪些 entries 该保留"的问题——用关键词匹配"重要"entries 是 workaround，换个语言/项目类型就失效。问题在于截断策略不区分信息的结构层次。

### 结构性不变量

对话历史中存在一个与内容无关的结构性不变量：**turn boundary**。

- 每个 user→assistant 交互的第一条 user 消息 = 用户的任务请求
- 紧跟其后的第一条 assistant 消息 = LLM 的规划/响应

这些 turn boundary entries 携带任务级语义（用户要做什么、LLM 打算怎么做），而后续的 tool calls 和 tool results 是执行细节。这个区分是结构性的——不依赖语言、关键词或项目类型。

### 三个子问题

1. **trimHistory**（保存时截断）：FIFO 不区分 turn boundaries 和执行细节
2. **TopicDetector**（话题切换检测）：context 构建取最后 8 条 entries，全是执行细节，与用户新消息零词汇重叠
3. **compactHistory**（token 超限时摘要）：把 JSON 序列化的 entries 扔给 LLM 做摘要，输入质量差

## 机制性修复

### 统一原则：Turn Boundary 分层

三个子问题共享同一个修复原则——用 turn boundary 做结构性分层，而不是用关键词做内容匹配。

### Fix 1: trimHistory 两层截断

**文件**：`gui/im_conversation_trim.go`

**机制**：将 entries 分为两层：
- Tier 1（turn boundaries）：每个 user→assistant 交互的第一条消息。预算 maxTier1=10。
- Tier 2（执行细节）：其余所有 entries。预算 MaxConversationTurns - outsideTier1。

截断时保留所有 tier-1 entries + 最近的 tier-2 entries，中间插入分隔符。

**为什么不是 workaround**：tier-1 的识别完全基于 role 序列（`prevRole != "user"` → 新 turn 的 user 消息；`prevRole == "user"` → 第一条 assistant 响应），不依赖任何关键词。任何语言、任何项目类型、任何工作流模板都适用。

### Fix 2: TopicDetector context 用 turn boundaries

**文件**：`gui/im_topic_detector.go`

**机制**：`detect()` 的 context 构建从"最后 8 条 user+assistant texts"改为"turn boundary texts + 最后 2 条 recency texts"。

**为什么不是 workaround**：旧方法取最后 8 条是一个隐含假设——"最近的 entries 代表当前话题"。在多工具调用场景下这个假设不成立。新方法用 turn boundaries 代表话题，这是结构性正确的——用户的任务请求和 LLM 的规划才是话题的定义。

### Fix 3: compactHistory 摘要输入用 turn boundaries

**文件**：`gui/im_message_handler.go`

**机制**：摘要的输入从 JSON 序列化的全部 entries 改为 turn boundary texts 的人类可读格式。每条 turn boundary 截断到 500 rune。

**为什么不是 workaround**：旧方法把 `{"role":"assistant","content":"...","tool_calls":[...]}` 格式的 JSON 扔给 LLM 做摘要，LLM 需要先理解 JSON 结构再提取语义。新方法直接给 LLM 人类可读的 `[user] 开发一个项目\n[assistant] 好的，我来规划...` 格式，摘要质量取决于 LLM 的语言理解能力而非 JSON 解析能力。

## 验收标准

1. 编程项目跨 100+ entries 后，trimHistory 保留所有 turn boundaries（用户请求 + LLM 规划）
2. TopicDetector 的 BM25 比较基于 turn boundaries 而非执行细节
3. compactHistory 的摘要输入是人类可读文本而非 JSON
4. 以上三点不依赖任何关键词——用完全随机的内容测试仍然成立（TestTrimHistory_StructuralInvariant_NoKeywordDependency）
5. 所有 17 个测试通过（6 trimHistory + 2 TopicDetector 新增 + 11 现有 TopicDetector）
6. GUI / corelib / TUI 编译通过
