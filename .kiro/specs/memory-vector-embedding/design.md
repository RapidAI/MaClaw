# Maclaw 记忆系统全面升级 — 技术设计

## 1. 整体架构

```
┌─────────────────────────────────────────────────────────────────┐
│                    corelib/memory/store.go                      │
│  ┌─────────┐ ┌────────────┐ ┌───────────┐ ┌─────────────────┐  │
│  │  BM25   │ │ VectorIndex│ │ MemGraph  │ │  MS Scorer      │  │
│  │  Index  │ │ (cosine)   │ │ (1-hop)   │ │  (融合评分)     │  │
│  └────┬────┘ └─────┬──────┘ └─────┬─────┘ └────────┬────────┘  │
│       └──────┬─────┘              │                │           │
│              ▼                    ▼                ▼           │
│         Relevance ──────► Graph Expand ──────► Final Score    │
├───────────────────────────────────────────────────────────────┤
│  ┌──────────┐ ┌────────────┐ ┌──────────────┐ ┌────────────┐  │
│  │Reflector │ │ Conflict   │ │ Forgetting   │ │ Episodic→  │  │
│  │(LLM归纳)│ │ Detector   │ │ Curve        │ │ Semantic   │  │
│  │          │ │ (LLM判断)  │ │ (强度衰减)   │ │ Promoter   │  │
│  └──────────┘ └────────────┘ └──────────────┘ └────────────┘  │
├───────────────────────────────────────────────────────────────┤
│  Entry: +Embedding +RelatedIDs +Strength +Scope +Status       │
└───────────────────────────────────────────────────────────────┘
         ▲ Embed()
┌────────┴──────────────────────────────────────────────────────┐
│  corelib/embedding/  (Embedder 接口 + CGO GemmaEmbedder)     │
└───────────────────────────────────────────────────────────────┘
```

## 2. Entry 类型扩展

```go
type Entry struct {
    ID          string    `json:"id"`
    Content     string    `json:"content"`
    Category    Category  `json:"category"`
    Tags        []string  `json:"tags"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
    AccessCount int       `json:"access_count"`
    // --- 新增字段 ---
    Embedding  []float32 `json:"embedding,omitempty"`   // F1: 向量
    RelatedIDs []string  `json:"related_ids,omitempty"` // F3: 关联图
    Strength   float64   `json:"strength,omitempty"`    // F5: 记忆强度
    Scope      Scope     `json:"scope,omitempty"`       // F7: 作用域
    Status     Status    `json:"status,omitempty"`      // F6: 状态(active/superseded/dormant)
}

type Scope string
const (
    ScopeGlobal  Scope = "global"
    ScopeProject Scope = "project"
)

type Status string
const (
    StatusActive     Status = ""           // 默认，空值=active
    StatusSuperseded Status = "superseded" // 被新记忆取代
    StatusDormant    Status = "dormant"    // 遗忘曲线低于阈值
)
```

## 3. F1: 向量检索

### 3.1 corelib/embedding/

```go
// embedder.go — 接口
type Embedder interface {
    Embed(text string) ([]float32, error)
    EmbedBatch(texts []string) ([][]float32, error)
    Dim() int
    Close()
}

// gemma_cgo.go (build tag: !noembedding)
// CGO 链接 librapidspeech_static.a
type GemmaEmbedder struct { ... }

// noop.go (build tag: noembedding)
type NoopEmbedder struct{}  // Embed() returns nil, err=nil
```

### 3.2 corelib/memory/vector_index.go

```go
type vectorIndex struct {
    mu         sync.RWMutex
    embeddings map[string][]float32
    dim        int
}
func (v *vectorIndex) score(queryEmb []float32) map[string]float64
```

### 3.3 评分融合

```
relevance = 0.4×bm25Score + 0.6×cosineSim + projectAffinity
```

## 4. F2: 记忆反思

### corelib/memory/reflector.go

```go
type Reflector struct {
    store *Store
    llm   LLMChatCaller
}

func (r *Reflector) Reflect(ctx context.Context) (*ReflectResult, error)
```

触发条件：记忆条目 > 50 且距上次反思 > 24h

流程：
1. 按 category 分组，取最近 30 条 episodic 记忆
2. LLM prompt: "从以下记忆中归纳用户的偏好、习惯和决策模式"
3. 解析 LLM 输出为 key-value 洞察
4. 每条洞察存为 `preference` 或 `instruction` 类型
5. 去重：如果已有相似洞察（embedding cosine > 0.9），更新而非新增

与 Compressor 的关系：Reflector 在 Compressor 之后运行，先压缩再反思。

## 5. F3: 记忆关联图

### corelib/memory/graph.go

```go
type memoryGraph struct {
    mu    sync.RWMutex
    edges map[string]map[string]float64  // id → {relatedID → strength}
}

func (g *memoryGraph) link(id1, id2 string, strength float64)
func (g *memoryGraph) expand(ids []string, hops int) []string
func (g *memoryGraph) rebuild(entries []Entry)
```

Save 时自动关联：
- 用 BM25 + embedding 找到 top-3 相关条目（cosine > 0.7）
- 建立双向边，强度 = cosine similarity
- 关联上限：每条记忆最多 5 条关联

Recall 时 1-hop 扩展：
- 直接匹配的条目 → 沿关联边扩展 → 去重 → 合并到候选集
- 扩展条目的分数 = 原始分数 × 边强度 × 0.5（衰减因子）

## 6. F4: 分层记忆

### 层级映射

| 层级 | Category | Recall 策略 |
|------|----------|------------|
| Semantic | self_identity, user_fact, preference, instruction | 始终召回，最高优先级 |
| Episodic | conversation_summary, session_checkpoint | 按 recency 衰减，token 预算独立 |
| Working | （不在 Store 中，是对话 history） | 由调用方管理 |

### Episodic → Semantic 提升

```go
type Promoter struct {
    store *Store
    llm   LLMChatCaller
}

func (p *Promoter) Promote(ctx context.Context) (int, error)
```

逻辑：
1. 扫描所有 episodic 记忆，用 embedding 聚类
2. 同一簇出现 ≥ 3 次的 fact → 候选提升
3. LLM 确认："以下事实在多次对话中反复出现，是否应提升为长期知识？"
4. 确认后存为 `preference` 或 `instruction`，scope=global

## 7. F5: 主动遗忘

### 遗忘曲线公式

```
strength(t) = S₀ × exp(-λ × hours_since_last_access)
```

- `S₀`：上次访问后的基础强度（每次 recall 命中 +1.0，初始 1.0）
- `λ = 0.003`：衰减率（约 14 天半衰期）
- 阈值：`strength < 0.1` → 标记为 dormant

### 替代 LRU

当前 `evictLRU()` 改为 `evictByStrength()`：
- dormant 条目不参与 Recall/RecallDynamic
- dormant 条目仍可通过 `Search()` / `List()` 显式查找
- 当总条目（含 dormant）超过 `maxItems × 1.5` 时，删除最弱的 dormant 条目

## 8. F6: 记忆冲突检测

### corelib/memory/conflict.go

```go
type ConflictDetector struct {
    store    *Store
    embedder embedding.Embedder
    llm      LLMChatCaller
}

func (d *ConflictDetector) Check(newEntry Entry) (*ConflictResult, error)
```

流程：
1. 用 embedding cosine 找到 top-5 最相似的已有条目（cosine > 0.8）
2. 如果无高相似条目，无冲突
3. LLM prompt: "新记忆: X\n已有记忆: Y\n这两条是否矛盾？回答 yes/no + 原因"
4. 如果矛盾：旧记忆 Status = superseded，新记忆正常保存
5. 如果不矛盾但高度相似：触发 compressor 的 merge 逻辑

降级：无 embedder 时用 BM25 找候选，无 LLM 时跳过冲突检测。

## 9. F7: 跨项目记忆共享

### Scope 自动推断

```go
func inferScope(category Category) Scope {
    switch category {
    case CategorySelfIdentity, CategoryUserFact, CategoryPreference, CategoryInstruction:
        return ScopeGlobal
    default:
        return ScopeProject
    }
}
```

### Recall 修改

`RecallForProject(msg, projectPath)` 中：
- `ScopeGlobal` 条目：始终参与，不受 projectPath 过滤
- `ScopeProject` 条目：仅当 Tags 包含 projectPath 时参与（现有逻辑）

## 10. 依赖关系与实施顺序

```
Phase 1 (基础设施):  F1(向量) + F7(Scope) + F5(遗忘曲线)
    ↓ Entry 类型扩展完成
Phase 2 (图+分层):   F3(关联图) + F4(分层)
    ↓ 需要 embedding 做聚类
Phase 3 (智能层):    F2(反思) + F6(冲突检测)
    ↓ 需要 LLM + embedding
```

## 11. 文件清单

| 文件 | 功能 |
|------|------|
| `corelib/embedding/embedder.go` | Embedder 接口 |
| `corelib/embedding/gemma_cgo.go` | CGO Gemma 桥接 |
| `corelib/embedding/noop.go` | 降级 stub |
| `corelib/memory/types.go` | Entry 扩展（+Embedding/RelatedIDs/Strength/Scope/Status） |
| `corelib/memory/vector_index.go` | 向量索引 |
| `corelib/memory/graph.go` | 记忆关联图 |
| `corelib/memory/reflector.go` | 记忆反思 |
| `corelib/memory/promoter.go` | Episodic→Semantic 提升 |
| `corelib/memory/forgetting.go` | 遗忘曲线 + 强度管理 |
| `corelib/memory/conflict.go` | 冲突检测 |
| `corelib/memory/store.go` | 集成所有增强到 Recall 流程 |
| `build/build_rapidspeech.cmd` | Windows 静态库构建 |
| `build/build_rapidspeech.sh` | Linux/macOS 静态库构建 |
