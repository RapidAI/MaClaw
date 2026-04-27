# Maclaw 记忆管理与工具调度技术详解

## 一、记忆管理系统

记忆系统位于 `corelib/memory/`，实现了一套多层级、多索引、带遗忘曲线的长期记忆架构。

### 1.1 核心数据结构

#### Entry（记忆条目）

```go
type Entry struct {
    ID          string      // 唯一标识（纳秒时间戳+随机hex）
    Content     string      // 记忆内容
    Category    Category    // 分类
    Tags        []string    // 标签（含项目路径用于作用域过滤）
    CreatedAt   time.Time
    UpdatedAt   time.Time
    AccessCount int         // 访问计数
    Embedding   []float32   // 向量嵌入（GemmaEmbedder 生成）
    RelatedIDs  []string    // 关联记忆 ID（图结构）
    Strength    float64     // 遗忘曲线强度值
    Status      Status      // active / superseded / dormant
    Scope       Scope       // global / project
    Pinned      bool        // 钉选保护，不被压缩/淘汰
    CompactForm string      // 压缩摘要，用于上下文注入
}
```

#### 记忆分类体系

采用双轨分类：传统分类 + Claude 风格四类分类。

| 传统分类 | 说明 | 重要性权重 | 作用域 |
|---|---|---|---|
| `self_identity` | 自我身份 | 4.0 | global |
| `instruction` | 用户指令/纠正 | 3.0 | global |
| `preference` | 用户偏好 | 2.0 | global |
| `project_knowledge` | 项目知识 | 2.0 | project |
| `session_checkpoint` | 会话检查点 | 1.5 | project |
| `conversation_summary` | 对话摘要 | 1.0 | project |

Claude 风格映射：`user` → `user_fact`，`feedback` → `instruction`，`project`/`reference` → `project_knowledge`。

#### MemGPT 风格层级

- **Semantic Tier**：抽象知识（user_fact, preference, instruction, self_identity）
- **Episodic Tier**：事件记录（conversation_summary, session_checkpoint）

### 1.2 存储引擎（Store）

```go
type Store struct {
    entries  []Entry           // 内存中的全部条目
    path     string            // 持久化路径（JSON 文件）
    maxItems int               // 容量上限：500
    bm25     *bm25Index        // BM25 全文检索索引
    vecIndex *vectorIndex      // 向量相似度索引
    graph    *memoryGraph      // 记忆关系图
    embedder embedding.Embedder // 嵌入模型
    archive  *ArchiveStore     // 冷存储（淘汰条目归档）
}
```

持久化采用异步写入：`signalSave()` 通过 channel 通知后台 `persistLoop` goroutine，延迟 5 秒合并写入，避免频繁 I/O。

### 1.3 三索引检索体系

#### BM25 索引
基于词频的全文检索，对记忆内容进行分词后构建倒排索引，支持增量更新。

#### 向量索引（vectorIndex）
存储 L2 归一化的嵌入向量，余弦相似度退化为点积运算：
```go
func (v *vectorIndex) score(queryEmb []float32) map[string]float64 {
    // 对每个条目计算 dot product
    sim := dotProduct(queryEmb, emb)
}
```

#### 记忆图（memoryGraph）
双向加权图结构，每个节点最多 5 条边：
- `link(id1, id2, strength)` 创建双向边
- 新边强度低于所有现有边时被拒绝
- `expand(seedIDs, hops)` BFS 扩展，每跳衰减 0.5×

### 1.4 记忆召回算法（Memory Stream Scoring）

灵感来自 Generative Agents 论文，融合三个信号：

```
Score = 1.0 × Recency + 1.0 × Importance + 1.0 × Relevance
```

各信号计算方式：

**Recency（时效性）**：
```
Recency = exp(-0.005 × hours_since_update)
```

**Importance（重要性）**：
```
Importance = CategoryWeight(category) + log(1 + access_count)
```

**Relevance（相关性）**：融合 BM25 + 向量检索
```
FusedRelevance = 0.4 × BM25_score + 0.6 × cosine_similarity + project_affinity(+3.0)
```

#### 召回流程（RecallForProject）

1. 计算 BM25 分数（store lock 外）
2. 计算向量分数（store lock 外）
3. 分类处理：
   - `self_identity` → 始终优先召回
   - `user_fact` → 次优先
   - 其他 → Memory Stream 评分排序
4. **1-hop 图扩展**：对 top 候选执行图扩展，发现关联记忆
5. Token 预算控制：最多 20 条，总 token ≤ 2000

#### LLM 二次过滤（RecallWithLLMFilter）

两阶段召回：
1. Stage 1：BM25+Vector+Graph 宽召回
2. Stage 2：LLM 从候选中精选最相关条目

### 1.5 遗忘曲线（Ebbinghaus Forgetting Curve）

```go
S(t) = S₀ × exp(-λ × hours)    // λ = 0.003, 半衰期 ≈ 9.6 天
```

- **休眠阈值**：强度 < 0.1 时标记为 `dormant`
- **召回增强**：每次被召回时 `Strength += 1.0`，`UpdatedAt` 重置
- **保护机制**：`self_identity` 类别永不衰减
- 活跃条目保留峰值 Strength，仅在变为休眠时持久化衰减值

### 1.6 后台维护流水线（Pipeline）

每 6 小时执行一次完整维护周期：

```
Decay → Compress → Promote → Reflect
```

#### Step 0: Decay（衰减）
`batchDecayAndMark()` 遍历所有条目，计算当前强度，标记休眠条目。

#### Step 1: Compress（压缩）

`Compressor` 执行多步压缩：

1. **备份**：压缩前创建快照备份
2. **精确去重**（dedup）：删除内容完全相同的条目
3. **语义去重**（mergeSemanticDuplicates）：LLM 判断语义相似条目并合并
4. **LLM 压缩**：对内容 ≥ 200 字符的非保护、非钉选条目，调用 LLM 压缩至 50% 以下
5. **CompactForm 回填**：为缺少摘要的条目生成紧凑表示
6. **熔断机制**：连续 3 次压缩失败后触发熔断，跳过直到下一个调度周期

压缩 Prompt 策略：
```
保留名称、数字、路径、命令、技术术语
删除填充词、冗余解释
使用简洁要点或短句
```

#### Step 2: Promote（晋升）

`Promoter` 实现 MemGPT 风格的 Episodic → Semantic 转换：

- 扫描最近 50 条 Episodic 记忆
- LLM 识别出现 ≥ 3 次的重复主题
- 将重复模式晋升为 Semantic 记忆（preference/instruction/user_fact）
- 每次最多晋升 5 条

#### Step 3: Reflect（反思）

`Reflector` 灵感来自 Generative Agents 的反思机制：

- 前置条件：总条目 ≥ 50，距上次反思 ≥ 24 小时
- 取最近 30 条 Episodic 记忆
- LLM 分析提取高层洞察：用户偏好、习惯、决策模式
- 生成的洞察存储为 Semantic 记忆

### 1.7 知识提取器（KnowledgeExtractor）

会话结束后从对话历史中提取知识点：

- **互斥机制**：检测最近 10 条消息中是否已有记忆写入信号（"已保存到记忆"、"memory:save" 等），避免重复
- **预压缩**：对话超过 20 轮时，先 LLM 压缩再提取
- **冷却时间**：默认 1 小时，防止频繁提取
- 提取的知识点按类别分类后存入 Store

### 1.8 归档系统（ArchiveStore）

冷存储，容量上限 1000 条：

- **淘汰归档**：GC 时将低强度条目移入归档
- **智能复活**：GC 周期中，根据 top-20 活跃记忆的标签和类别，从归档中找回相关条目
- **持久化**：独立 JSON 文件（`archive.json`），异步写入

---

## 二、工具调度系统

工具系统位于 `corelib/tool/`，实现了上下文感知的智能工具选择与路由。

### 2.1 工具注册（Registry）

```go
type RegisteredTool struct {
    Name        string                 // 工具名
    Description string                 // 描述
    Category    Category               // builtin / mcp / skill / non_code
    Tags        []string               // 标签（用于分组激活）
    Priority    int                    // 优先级
    Status      Status                 // available / degraded / unavailable
    InputSchema map[string]interface{} // JSON Schema
    Body        string                 // 工具实现体
    BodySummary string                 // 实现体摘要（用于检索增强）
    Caps        CapRequirement         // 平台能力需求（显示器/剪贴板/网络）
    Handler     Handler                // 执行函数
}
```

`Registry` 线程安全，支持注册/注销/查询/变更回调。注册时自动填充 `Body` 和 `BodySummary`。

### 2.2 工具定义生成（DefinitionGenerator）

动态合并三类工具定义：

1. **Builtin 工具**：静态定义（bash, read_file, memory, web_search 等）
2. **远程 MCP 工具**：从健康的 MCP Server 动态获取
3. **本地 MCP 工具**：从 stdio MCP Server 获取

**延迟加载（Deferred Tools）**：部分工具不包含在初始 prompt 中，通过 `SearchDeferred` 按需发现（灵感来自 Claude Code 的 ToolSearchTool 模式）。

### 2.3 核心工具集

始终包含在路由结果中的核心工具：

```
bash, read_file, write_file, list_directory,
send_and_observe, create_session, list_sessions,
get_session_output, get_session_events, control_session,
call_mcp_tool, list_skills, run_skill,
screenshot, send_file, memory,
web_search, web_fetch, set_nickname,
browser_connect, browser_navigate, browser_click,
discover_tool
```

### 2.4 工具路由器（Router）

当工具总数超过 `MaxToolBudget`（28）时启动智能路由：

#### 路由流程

```
1. 分离核心工具（始终保留）和候选工具
2. BM25 索引候选工具描述
3. 混合检索融合（BM25 + Vector）
4. 三信号评分
5. LLM Rerank（可选）
6. 截断至预算
7. 技能推荐注入
```

#### 三信号评分公式

```
Score = α × Retrieval + β × Experience + γ × Priority

有 UsageTracker 时：α=0.6, β=0.3, γ=0.1
无 UsageTracker 时：α=0.9, γ=0.1
```

**Retrieval Score**：BM25 + Vector 融合后 min-max 归一化
**Experience Score**：基于历史使用记录的经验分
**Priority Bonus**：`clamp(priority × 0.1, 0, 1)`

#### 动态工具上限

- `MaxToolBudget = 28`：发送给 LLM 的最大工具数
- `MaxDynamicRouted = 18`：非核心动态工具的最大数量

### 2.5 混合检索器（HybridRetriever）

融合 BM25（词法）和向量（语义）检索分数：

```
FusedScore = α × normalize(BM25) + (1-α) × cosine_similarity
```

默认 `α = 0.4`（BM25 权重），`1-α = 0.6`（向量权重）。

#### 嵌入缓存

- **ToolEmbeddingCache**：工具描述嵌入的磁盘持久化缓存，按模型指纹隔离
- **QueryEmbeddingCache**：查询嵌入的 LRU 内存缓存，带 TTL 过期

### 2.6 使用追踪器（UsageTracker）

记录工具调用历史（滚动窗口 2000 条），持久化到 `~/.maclaw/data/tool_usage.json`。

#### 经验分算法（ExperienceScore）

```
对每条匹配记录：
  1. Jaccard 相似度 = |query_tokens ∩ record_tokens| / |query_tokens ∪ record_tokens|
  2. 时效衰减 = exp(-0.01 × hours)
  3. 成功权重 = success ? 1.0 : -0.3
  4. weighted_sum += jaccard × recency × success_weight

score = clamp(weighted_sum / count, 0, 1)
```

最多回溯 200 条匹配记录，避免计算开销过大。

### 2.7 LLM Reranker

当候选工具超过预算时，取 top-20 候选进行 LLM listwise 重排序：

```go
type Reranker interface {
    Rerank(userMessage string, candidates []CandidateSummary, topK int) ([]string, error)
}
```

输入包含工具名、描述、BodySummary；输出 top-5 工具名。重排序结果提升到候选列表前端，其余按融合分数补充。

### 2.8 分组激活（Group Activation）

用户消息中的关键词触发工具组批量激活：

| 关键词 | 激活标签 |
|---|---|
| 数据库 / database | database, sql, query, db |
| git / 版本控制 | git, vcs, version |
| 浏览器 / browser | browser, web, automation, test |
| 记忆 / memory | memory |
| 搜索 / search | web, search, internet, fetch |
| gui / 桌面 | gui, test, automation, desktop |
| 定时 / schedule | schedule, task, cron, timer |
| 配置 / config | config, settings |

支持中英文双语关键词。匹配到的工具组跳过评分直接包含。

### 2.9 工具增强（EnrichmentStore）

为工具生成增强搜索文本，提升检索召回率：

- 持久化到 `~/.maclaw/data/tool_enrichment.json`
- LLM 生成额外查询词（用户可能搜索的关键词）
- 检索时优先使用增强文本替代原始 name+description

### 2.10 工具选择器（Selector）

推荐最佳编程工具（claude/codex/gemini/cursor/opencode/iflow/kilo）：

- 每个工具有能力画像：支持语言、框架、任务类型、基础分
- 使用 BM25 对任务描述与能力文本进行匹配
- 结合已安装工具列表过滤

### 2.11 路由日志

详细路由决策记录到 `~/.maclaw/logs/tool_route.log`：

```
=== Tool Route [2026-04-02 10:30:00] ===
Message: 帮我写一个数据库查询...
Total tools: 45 | Core: 22 | Candidates: 23 | Hybrid: true
Body-aware: true
Top-20 candidates by fused score:
  #1 sql_query = 0.8923
  #2 db_connect = 0.7654
  ...
Selected tools (28):
  - bash
  - read_file
  ...
Reranker output (5): #1 sql_query #2 db_connect ...
```

日志文件超过 5MB 自动截断。

---

## 三、Agent 循环与并发控制

### 3.1 LoopContext（循环上下文）

每个 Agent 循环（聊天或后台）拥有独立的 LoopContext：

```go
type LoopContext struct {
    ID            string        // 唯一标识
    Kind          LoopKind      // Chat / Background
    SlotKind      SlotKind      // Coding / Scheduled / Auto / SSH / Browser / GUI
    Conversation  []interface{} // 当前对话消息
    History       []interface{} // 加载的历史
    maxIterations int           // 最大迭代次数
    iteration     int           // 当前迭代
    status        string        // running / paused / completed / failed
}
```

### 3.2 BackgroundLoopManager（后台循环管理器）

基于槽位的并发控制：

| 槽位类型 | 最大并发 |
|---|---|
| Coding | 2 |
| Scheduled | 1 |
| Auto | 1 |
| SSH | 10 |
| Browser | 2 |
| GUI | 1 |

- `Spawn()`：有空槽则创建，满则返回 nil
- `SpawnOrQueue()`：有空槽则创建，满则排队等待
- `Complete()`：释放槽位，自动调度队列中的等待任务
- `OnChange` 回调通知 UI 更新

### 3.3 LLM 集成

支持 OpenAI 和 Anthropic 双协议：

- `DoOpenAIRequest()`：非流式工具调用请求
- `DoStreamOpenAIRequest()`：流式 SSE 请求
- `DoSimpleLLMRequest()`：简单聊天请求（无工具调用）
- 自动处理 SSE 格式检测、`<think>` 标签剥离、function_call 块清理
- Provider 适配：MiniMax 等需要 system-role 合并的特殊处理

---

## 四、架构设计亮点

1. **Memory Stream 三信号融合**：时效性 + 重要性 + 相关性，平衡新旧记忆的召回
2. **图扩展召回**：1-hop BFS 发现关联记忆，突破关键词匹配的局限
3. **Ebbinghaus 遗忘曲线**：模拟人类记忆衰减，自动淘汰过时信息
4. **Episodic → Semantic 晋升**：重复出现的事件记忆自动提炼为持久知识
5. **三信号工具评分**：检索 + 经验 + 优先级，兼顾语义匹配和使用习惯
6. **分组激活 + 延迟加载**：减少初始 prompt 的工具数量，按需激活
7. **LLM Reranker**：对 top 候选进行精排，提升工具选择准确率
8. **熔断与归档复活**：压缩失败熔断保护，GC 时智能复活归档条目
9. **槽位并发控制**：不同任务类型独立限流，支持排队调度
10. **互斥知识提取**：避免主 Agent 和后台提取器产生重复记忆
