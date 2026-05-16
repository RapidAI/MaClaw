# 自动话题切换 — 技术设计

## 架构概览

在 `gui/im_message_handler.go` 的消息处理入口（HandleIMMessageWithProgressAndStream）中，
在构建 system prompt 之前插入一个话题切换检测步骤。

```
用户消息 → 话题切换检测 → [如果切换] 清理 + 摘要存档 → 正常处理
                        → [如果延续] 正常处理
```

## 核心组件

### 1. TopicSwitchDetector

新增文件：`gui/im_topic_detector.go`

```go
type TopicSwitchDetector struct {
    bm25Threshold     float64 // 低于此值进入模糊地带，默认 1.0
    bm25ClearThreshold float64 // 低于此值直接判定为新话题，默认 0.3
    timeDecayMinutes  float64 // 时间衰减起点，默认 30
    llmClient         *http.Client
    llmConfig         func() LLMConfig
}

type TopicDecision int
const (
    TopicSame TopicDecision = iota
    TopicNew
)
```

### 2. 检测流程

```
1. 从 conversationMemory 取最近 5 轮用户消息，拼接为 "上下文文本"
2. 如果没有历史消息 → TopicSame（首条消息）
3. 用 BM25 计算新消息 vs 上下文文本的相似度 score
4. 计算时间衰减系数：
   elapsed = time.Since(lastAccess)
   decay = max(0, 1 - elapsed.Minutes() / timeDecayMinutes)
   adjustedScore = score * decay
5. 判定：
   - adjustedScore > bm25Threshold → TopicSame
   - adjustedScore < bm25ClearThreshold → TopicNew
   - 否则 → 调用 LLM 确认
```

### 3. LLM 确认调用

极短 prompt，约 50 tokens：

```
系统: 判断用户的新消息是否延续之前的对话话题。只回答 same 或 new。
之前的话题: {最近一轮用户消息的前100字}
新消息: {新消息的前100字}
```

解析响应：包含 "new" → TopicNew，否则 → TopicSame。

### 4. 清理 + 摘要存档

当判定为 TopicNew 时：
1. 取当前 conversationMemory 的最近几轮
2. 生成一句话摘要（可复用 memory compressor 的逻辑）
3. 存入 memoryStore，category = conversation_summary
4. 调用 h.memory.clear(userID)

### 5. 集成点

在 `HandleIMMessageWithProgressAndStream` 中，slash 命令处理之后、
LLM 配置检查之后，插入：

```go
if decision := h.topicDetector.Detect(msg.Text, msg.UserID, h.memory); decision == TopicNew {
    h.archiveAndClear(msg.UserID)
}
```

## 性能考量

- BM25 计算：微秒级，用现有 corelib/bm25 包，无额外依赖
- LLM 确认：仅在模糊地带触发，预计 <10% 的消息会走到这步
- 摘要生成：复用 memory compressor，不额外调用 LLM（用简单截断）
- 整体延迟增加：BM25 路径 <1ms，LLM 路径 ~500ms（可接受）

## 阈值调优

初始值基于经验，后续可通过日志分析调整：
- bm25Threshold = 1.0（高于此值几乎确定是同一话题）
- bm25ClearThreshold = 0.3（低于此值几乎确定是新话题）
- timeDecayMinutes = 30（30分钟后开始衰减）
