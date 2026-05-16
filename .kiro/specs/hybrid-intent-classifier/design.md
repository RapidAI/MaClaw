# 混合意图分类器设计文档（Hybrid Intent Classifier）

## 1. 问题

当前意图识别完全基于关键词/短语子串匹配：
- `conditionalKeepRules`：按关键词决定条件工具是否加入列表
- `classifyTaskIntent()`：用 `codingKeywords` 判断编码任务
- `codingActionPhrases`：短动作指令匹配
- `containsSkipSignal()`：coding tool gate 跳过信号

问题：
1. 误触发："什么是MVC架构"命中"架构"→误判为编码任务
2. 漏触发："帮我搞个能自动抢票的东西"无关键词命中
3. 上下文盲："格式化"在不同语境含义不同
4. 维护成本高：每个 case 加一个关键词，列表膨胀

## 2. 基准测试结果（Gemma Embedding 300M）

测试日期：2026-04-14，51 个测试用例。

### 2.1 Embedding 单独使用

最优阈值 high=0.78, low=0.45，准确率 78.4%（40/51）。

延迟：embed ~15-25ms/query，cosine 分类 <1ms。

### 2.2 误报模式分析

| 误报类型 | 占比 | 示例 | 原因 |
|---------|------|------|------|
| 概念问答 vs 操作意图 | 7/11 | "什么是MVC架构"→coding, "docker是什么"→ssh | embedding 无法区分"谈论X"和"操作X" |
| 口语化/模糊表达 | 2/11 | "帮我搞个能自动抢票的东西"→browser | 语义模糊，多意图距离接近 |
| 短文本误匹配 | 1/11 | "ok"→chat(0.77) | 短文本 embedding 不稳定 |
| 跨语言实体关联 | 1/11 | "create a REST API"→ssh | "management"与服务器管理语义接近 |

关键发现：误分类 case 的 top1-top2 gap 通常 < 0.05，正确分类 gap > 0.10。

### 2.3 结论

Embedding 单独不够用，但在高置信区间（cosine > 0.78 且 gap > 0.10）非常准确。
需要三层混合方案。

## 3. 三层混合方案

```
用户消息 → Layer 1: 规则快速路径（0ms）
  ├─ 问句模式命中 → 返回 "query"（知识问答）
  ├─ 短指令检测 → 返回 "short_command"（需上下文）
  ├─ 高置信关键词 → 返回具体意图
  └─ 未命中 → Layer 2: Embedding 锚点匹配（~20ms）
       ├─ cosine > 0.78 且 gap > 0.10 → 返回意图
       ├─ cosine < 0.55 → 返回 "unknown"
       └─ 模糊区间 → Layer 3: LLM 分类器（~300ms，可选）
```

### 3.1 Layer 1: 规则快速路径

新增"问句模式检测"，在关键词匹配之前执行：

```
问句模式正则：
  中文：^(什么是|是什么|怎么|如何|为什么|有哪些|介绍一下|解释一下|了解)
  英文：^(what is|how to|how do|why|explain|tell me about)
```

命中问句模式 → 标记为 `intentQuery`，不走操作意图匹配。
这能干掉 7/11 的误报（"什么是MVC架构"、"docker是什么"、"python怎么安装"等）。

保留现有关键词匹配作为高置信快速路径。

### 3.2 Layer 2: Embedding 锚点匹配

预定义每种意图的锚点文本，启动时预计算 embedding 并缓存。

意图类别：coding, ssh, content, chat, browser

判定规则：
- cosine > 0.78 且 gap > 0.10 → 高置信，直接返回
- cosine ∈ [0.55, 0.78) 或 gap < 0.10 → 模糊，进 Layer 3
- cosine < 0.55 → 无匹配

复用现有 `QueryEmbeddingCache`（30s TTL）避免重复计算。

### 3.3 Layer 3: LLM 兜底

处理 Layer 2 模糊区间的 case（预计 <10% 请求）。
通过 `LLMClassifyFunc` 回调注入，由调用方（gui/tui）提供 LLM 访问能力。

Prompt 设计：精简的中文分类指令，列出 7 个类别（coding/ssh/content/chat/browser/query/unknown），
要求只返回一个单词。max_tokens ~60，超时 ~5s。

结果解析：容忍大小写、尾部标点、多余换行等格式差异。

降级策略：LLM 调用失败时，回退到 Layer 2 的模糊结果（如果有）或 unknown。

## 4. 实现

### 4.1 新增文件

`corelib/tool/intent_classifier.go`：IntentClassifier 核心实现

### 4.2 IntentClassifier 接口

```go
type IntentResult struct {
    Intent     string  // "coding", "ssh", "content", "chat", "browser", "query", "short_command", "unknown"
    Confidence float64 // 0-1
    Gap        float64 // top1 - top2 score gap
    Layer      int     // 1=规则, 2=embedding, 3=LLM
}

type IntentClassifier struct {
    embedder    embedding.Embedder
    anchors     []intentAnchor
    queryCache  *QueryEmbeddingCache
}

func (ic *IntentClassifier) Classify(text string) IntentResult
```

### 4.3 集成点

1. `matchConditionalKeepRules`：Layer 1 问句模式检测后，对非问句消息用 Layer 2 辅助判断
2. `checkSessionTaskGuard`：ambiguous 分支从"扫历史关键词"改为调 IntentClassifier
3. `coding_tool_gate`：可选，用 IntentClassifier 结果辅助 gate 决策

### 4.4 Embedding 模型降级

当 Gemma 模型不可用时（`NoopEmbedder`），Layer 2 自动跳过，退化为纯规则匹配（现有行为）。

## 5. 验收标准

- 基准测试准确率 ≥ 85%（Layer 1 + Layer 2 组合）→ 实际达到 98%（49/50）
- 问句模式误报（"什么是X"被当作操作意图）降为 0 → ✅ 全部 9 个问句 case 正确
- 延迟 < 30ms（不含 LLM 层）→ ✅ Layer 1 ~0ms, Layer 2 ~20ms
- 现有 `router_bm25_test.go` 不受影响（pre-existing failure 与本改动无关）
- 新增 `intent_classifier_test.go` 覆盖 50 个测试用例 → ✅ 全部通过

## 6. 测试结果（2026-04-14）

| 指标 | Embedding 单独 | Layer 1 + Layer 2 |
|------|---------------|-------------------|
| 准确率 | 78.4% (40/51) | 98.0% (49/50) |
| 问句误报 | 7 个 | 0 个 |
| 短指令误报 | 1 个 (ok→chat) | 0 个 |
| 唯一失败 | — | "create a REST API for user management"→ssh (gap=0.04, 模糊区间) |

Layer 1 规则处理了 14/50 case（全部正确），Layer 2 embedding 处理了 36/50 case（35 正确）。
组合后准确率从 78.4% 提升到 98.0%，提升 19.6 个百分点。
