# 记忆召回增强 — 需求文档

## 问题本质

maclaw 的记忆系统存在一类通用的「意图-记忆 gap」：用户用自然语言间接引用了某个已记忆的事物，但自动召回机制匹配不上。只有当用户明确要求「回忆一下」时，LLM 才会主动调用 `memory(action: recall)` 工具用精确关键词搜索，此时才能找到。

典型场景（不限于 4090）：
- 「登录 4090 服务器检查 GPU 占用率」→ 记忆里存的是 "4090 服务器 IP 192.168.x.x SSH 端口 22"
- 「用上次那个部署脚本」→ 记忆里存的是 "deploy.sh 在 /opt/scripts 目录下"
- 「帮我看看张三的项目进度」→ 记忆里存的是 "张三负责 payment-service 模块"
- 「连上测试环境跑一下」→ 记忆里存的是 "测试环境地址 test.example.com 端口 8080"
- 「用之前配好的 Claude key」→ 记忆里存的是 "Claude API key 存在 ~/.config/claude/key"

共同特征：用户消息中的关键实体（服务器名、人名、环境名、工具名）与记忆条目有语义关联，但 BM25 全文匹配因噪声词稀释而失效。

## 根因分析（通用）

### 1. 被动检索 vs 主动理解
当前架构是「被动检索式」：把整句用户消息当字符串丢给 BM25+向量，不理解用户在引用什么。缺少「意图级触发」——系统不会识别出「用户提到了一个特定实体，我应该去找关于它的信息」。

### 2. 全句 BM25 的噪声稀释
用户消息中的动词、助词、描述性词语（"登录"、"检查"、"帮我"、"用一下"）在 BM25 中与关键实体（"4090"、"张三"、"测试环境"）平等参与打分，稀释了关键实体的匹配权重。

### 3. 记忆注入架构缺陷
当前 `appendMemorySection` 只注入 user_fact 摘要 + recall 工具提示。project_knowledge、instruction 等类型完全依赖 LLM 自己决定是否调用 recall 工具。LLM 在大多数情况下不会主动调用——它不知道记忆里有什么。

### 4. Token 预算固定
`RecallForProject` 硬编码 maxTokens=2000，self_identity + user_fact 优先占位后，其他类型记忆可能排不进去。随着记忆增长，这个问题会越来越严重。

### 5. Tag 信号未利用
记忆条目的 tags 是高质量的结构化信号（存储时由 LLM 或用户显式标注），但当前 RRF 融合中只用了 project path 做 tag 匹配，没有利用 tags 与用户消息的交叉匹配。

## 需求列表

### REQ-1: 查询理解与扩展（Query Understanding）
- 从用户消息中提取关键实体和短语（人名、设备名、环境名、路径、IP、专有名词等）
- 用提取出的实体作为独立查询，与原始全句查询并行检索
- 合并多路检索结果，取每个条目的最高匹配分
- 纯规则实现，不依赖 LLM，延迟 < 5ms
- 覆盖中英文混合场景

### REQ-2: Tag 交叉匹配信号
- 将用户消息分词后与记忆条目的 tags 做交叉匹配
- 匹配命中的条目获得独立于 BM25/向量的额外 boost
- 作为第三路信号加入 RRF 融合
- 支持模糊匹配（tag "4090服务器" 匹配消息中的 "4090"）

### REQ-3: 动态 Token 预算与类型配额
- 根据活跃记忆数量动态调整 maxTokens 和 maxEntries
- 为 project_knowledge/instruction 类型保留最低预算配额
- 防止 user_fact 膨胀后完全挤出其他类型

### REQ-4: Proactive Recall（主动召回注入）
- 在构建 system prompt 时，基于用户消息自动执行一次召回
- 将命中的非 user_fact 记忆直接注入 system prompt
- LLM 不再需要自己决定是否调用 recall 工具来获取基础上下文
- 保留手动 recall 工具作为补充（LLM 仍可主动搜索更多记忆）

### REQ-5: LLM Reranker 可选增强
- 在有 LLM 可用时，对宽泛候选集做精选 rerank
- LLM 不可用时优雅降级到纯 BM25+Vector+Tag
- 控制调用频率，避免每条消息都触发 LLM rerank

## 非功能需求

- 所有改动向后兼容，不改变 memory.json 格式
- Query Understanding 延迟 < 5ms（纯规则）
- Proactive Recall 最多注入 8 条，控制 prompt 膨胀
- LLM Reranker 可配置开关，默认关闭
- 现有测试不能 break
