# MacLaw 多跳事实推理（Multi-Hop Fact Reasoning）设计文档

## 一、现状分析

### 1.1 当前能力

MacLaw 的记忆系统有三层图结构：

| 层 | 实现 | 能力 | 局限 |
|---|------|------|------|
| `memoryGraph` | entry-to-entry 加权边 | 1-hop BFS 扩展，找到相关 entry | 不理解关系语义 |
| `SemanticGraph` | subject-predicate-object 三元组 | 最多 3-hop 遍历，按关系类型加权 | 只返回已有 entry，不推导新结论 |
| `EntityIndex` | entity → entry 共现索引 | 找到共现实体 | 纯统计，无推理 |

### 1.2 核心缺口

**当前系统是"多跳检索"（multi-hop retrieval），不是"多跳推理"（multi-hop reasoning）。**

示例：
- 记忆 A：`"张三在 RapidAI 工作"` → 三元组 `(张三, works_at, RapidAI)`
- 记忆 B：`"RapidAI 的办公室在杭州"` → 三元组 `(RapidAI, located_in, 杭州)`

当前行为：查询"张三在哪个城市"时，`SearchWithOptions` 从"张三"出发，沿 `works_at` 边到达"RapidAI"，再沿 `located_in` 边到达"杭州"——但它只返回记忆 B 的 entry（"RapidAI 的办公室在杭州"），**不会生成"张三在杭州"这个推导结论**。LLM 需要自己从两条记忆中推理出答案。

**问题**：当推理链超过 2 跳，或中间节点的 entry 被 token budget 截断时，LLM 无法完成推理。

### 1.3 设计目标

在 `SemanticGraph` 之上新增推理层，能够：
1. 从已有三元组中推导出新的隐含事实（派生事实）
2. 将派生事实以可解释的方式注入 recall 结果
3. 不增加 LLM 调用开销（纯规则推理，<5ms）
4. 不修改已有的 `RecallDynamic` 管道结构

---

## 二、推理机制设计

### 2.1 推理类型

基于知识图谱推理的经典分类，MacLaw 需要支持以下三种：

#### Type 1: 传递性推理（Transitive Inference）

某些关系具有传递性：如果 `A rel B` 且 `B rel C`，则 `A rel C`。

可传递关系（从现有 `SemanticRelationSchema` 中筛选）：
- `located_in`：A 在 B，B 在 C → A 在 C
- `belongs_to`：A 属于 B，B 属于 C → A 属于 C
- `part_of`：A 是 B 的一部分，B 是 C 的一部分 → A 是 C 的一部分
- `depends_on`：A 依赖 B，B 依赖 C → A（传递性）依赖 C
- `manages`：A 管理 B，B 管理 C → A（间接）管理 C

#### Type 2: 组合推理（Compositional Inference）

不同关系的组合产生新关系：
- `works_at` + `located_in` → `based_in`：张三在 RapidAI 工作，RapidAI 在杭州 → 张三 based_in 杭州
- `uses` + `version_of` → `uses_version`：项目用 Python，Python 版本 3.11 → 项目用 Python 3.11
- `created_by` + `works_at` → `created_at`：工具由张三创建，张三在 RapidAI → 工具 created_at RapidAI

#### Type 3: 别名传递（Alias Transitivity）— 已有

`SemanticGraph` 已实现 `alias_of`/`same_as` 的传递闭包（`rebuildAliasesLocked`）。这是最简单的推理形式，无需额外工作。

### 2.2 推理规则定义

```go
// InferenceRule defines a single multi-hop reasoning rule.
type InferenceRule struct {
    // Name is a human-readable identifier for debugging/explainability.
    Name string

    // Type: "transitive" or "compositional"
    Type string

    // For transitive rules: the relation that is transitive.
    Relation string

    // For compositional rules: the two input relations and the output relation.
    InputRelation1 string // first hop relation
    InputRelation2 string // second hop relation
    OutputRelation string // derived relation

    // MaxChainLength limits transitive closure depth (default 3).
    MaxChainLength int

    // Confidence is the decay factor per hop (0.0-1.0).
    // Derived facts have confidence = base * Confidence^hops.
    ConfidenceDecay float64

    // Bidirectional: if true, also check reverse direction.
    Bidirectional bool
}
```

### 2.3 派生事实（Derived Fact）

```go
// DerivedFact is a fact inferred from existing SemanticFacts via rules.
// It is NOT persisted — it is computed on-demand during recall.
type DerivedFact struct {
    Subject   string
    Predicate string
    Object    string
    
    // Provenance: the chain of source facts that produced this derivation.
    SourceFacts []SemanticFact
    
    // The rule that produced this fact.
    RuleName string
    
    // Confidence score (decays with chain length).
    Confidence float64
    
    // Human-readable explanation of the reasoning chain.
    Explanation string
}
```

### 2.4 推理引擎

```go
// InferenceEngine performs rule-based multi-hop reasoning over SemanticGraph.
type InferenceEngine struct {
    rules []InferenceRule
    graph *SemanticGraph
}

// Infer takes a set of query entities and returns derived facts
// that are reachable through the registered rules.
func (ie *InferenceEngine) Infer(queryEntities []string, opts InferenceOptions) []DerivedFact
```

**算法**：
1. 从 query entities 出发，收集所有直接关联的 facts（1-hop）
2. 对每个 fact，检查是否有 rule 的 `InputRelation1` 匹配
3. 如果匹配，从 fact 的 Object 出发，查找 `InputRelation2` 的 facts
4. 找到匹配的第二跳 fact 后，生成 `DerivedFact`
5. 对传递性规则，递归到 `MaxChainLength`
6. 去重：相同 (Subject, Predicate, Object) 只保留 Confidence 最高的

**性能约束**：
- 最大遍历 facts 数：200（与 `SearchWithOptions` 的 `MaxVisitedFacts=500` 同量级）
- 最大派生 facts 数：20
- 最大链长：3（超过 3 跳的推理可靠性急剧下降）

---

## 三、接入点设计

### 3.1 接入 RecallDynamic

在 `RecallDynamic` 的 `semanticScores` 计算之后、RRF 融合之前，调用推理引擎：

```go
// In RecallDynamic, after SemanticGraph.SearchWithOptions:
var derivedFacts []DerivedFact
if s.inferenceEngine != nil {
    derivedFacts = s.inferenceEngine.Infer(expanded.Entities, InferenceOptions{
        Now:         time.Now(),
        OwnerID:     firstOwnerID(ownerID...),
        ProjectPath: projectPath,
        MaxDerived:  10,
    })
}
```

### 3.2 派生事实注入方式

派生事实不是 entry（没有 ID、没有持久化），不能直接参与 RRF 融合。注入方式：

**方案 A：Boost 源 entry 分数**

对产生派生事实的源 entry，给予额外的 score boost：
```go
for _, df := range derivedFacts {
    for _, sourceFact := range df.SourceFacts {
        if sc, ok := semanticScores[sourceFact.EntryID]; ok {
            semanticScores[sourceFact.EntryID] = sc + df.Confidence * 2.0
        }
    }
}
```

优点：不改变 RecallDynamic 的返回类型。
缺点：LLM 仍需自己从源 entry 中推理。

**方案 B：生成推理摘要注入 system prompt（推荐）**

将派生事实格式化为自然语言，作为独立 section 注入 proactive recall 的输出：

```
[推理链（自动推导）]
• 张三 → works_at → RapidAI → located_in → 杭州 ∴ 张三可能在杭州工作
  (置信度: 0.72, 来源: 记忆#A + 记忆#B)
```

优点：LLM 直接看到推导结论，不需要自己推理。
缺点：占用 token budget（每条推理链约 30-50 token）。

**推荐方案 B**，原因：
1. 多跳推理的价值正是"帮 LLM 省去推理步骤"
2. 30-50 token/条 × 最多 5 条 = 150-250 token，在 3000 token 的 proactive recall budget 内可控
3. 置信度和来源标注让 LLM 知道这是推导而非确定事实

### 3.3 注入位置

`gui/im_system_prompt.go` 的 `appendProactiveRecall()` 末尾：

```go
// After normal proactive recall entries are appended:
if len(derivedFacts) > 0 {
    sb.WriteString("\n[推理链（自动推导，非确定事实）]\n")
    for _, df := range derivedFacts[:min(5, len(derivedFacts))] {
        sb.WriteString("• " + df.Explanation + "\n")
    }
}
```

---

## 四、规则注册表

### 4.1 内置规则

```go
var BuiltinInferenceRules = []InferenceRule{
    // 传递性规则
    {Name: "located_in_transitive", Type: "transitive", Relation: "located_in", MaxChainLength: 3, ConfidenceDecay: 0.85},
    {Name: "belongs_to_transitive", Type: "transitive", Relation: "belongs_to", MaxChainLength: 3, ConfidenceDecay: 0.85},
    {Name: "part_of_transitive", Type: "transitive", Relation: "part_of", MaxChainLength: 3, ConfidenceDecay: 0.80},
    {Name: "depends_on_transitive", Type: "transitive", Relation: "depends_on", MaxChainLength: 2, ConfidenceDecay: 0.70},

    // 组合推理规则
    {Name: "works_at+located_in=based_in", Type: "compositional", InputRelation1: "works_at", InputRelation2: "located_in", OutputRelation: "based_in", ConfidenceDecay: 0.75},
    {Name: "uses+version_of=uses_version", Type: "compositional", InputRelation1: "uses", InputRelation2: "version_of", OutputRelation: "uses_version", ConfidenceDecay: 0.80},
    {Name: "created_by+works_at=created_at_org", Type: "compositional", InputRelation1: "created_by", InputRelation2: "works_at", OutputRelation: "created_at_org", ConfidenceDecay: 0.70},
    {Name: "deployed_on+located_in=deployed_in", Type: "compositional", InputRelation1: "deployed_on", InputRelation2: "located_in", OutputRelation: "deployed_in", ConfidenceDecay: 0.75},
    {Name: "manages+works_at=manages_at", Type: "compositional", InputRelation1: "manages", InputRelation2: "works_at", OutputRelation: "manages_at", ConfidenceDecay: 0.65},
}
```

### 4.2 扩展方式

未来可通过 steering 文件或配置声明自定义规则：
```yaml
# ~/.maclaw/steering/inference-rules.md
---
inclusion: always
---
# 自定义推理规则
- name: "hosts+runs_on=hosts_service"
  type: compositional
  input1: hosts
  input2: runs_on
  output: hosts_service
  confidence: 0.70
```

---

## 五、安全约束

### 5.1 置信度衰减

每跳推理置信度乘以 `ConfidenceDecay`：
- 1 跳：0.85
- 2 跳：0.85² = 0.72
- 3 跳：0.85³ = 0.61

低于 0.50 的派生事实不注入 system prompt。

### 5.2 矛盾检测

推理引擎在生成派生事实前，检查是否与已有事实矛盾：
- 已有 `(张三, located_in, 北京)` 且 `Negated=false`
- 推导出 `(张三, based_in, 杭州)`
- 两者矛盾 → 标记为 `conflicted`，不注入或标注"⚠️ 与已有记忆冲突"

复用 `SemanticGraph` 已有的 `semanticIsDominanceRelation` + `semanticHasPolarityCompetition` 机制。

### 5.3 时效性

派生事实继承源 facts 中最严格的时效约束：
- 源 fact A 的 `InvalidAt = 2026-03-01`
- 源 fact B 无时效约束
- 派生事实的 `InvalidAt = 2026-03-01`（取最早的失效时间）

### 5.4 OwnerID 隔离

推理链中的所有源 facts 必须属于同一个 OwnerID（或为空/共享）。不允许跨用户推理。

---

## 六、实现计划

### Phase 1: 推理引擎核心（`corelib/memory/inference_engine.go`）

- `InferenceRule` 和 `DerivedFact` 类型定义
- `InferenceEngine` 结构体 + `Infer()` 方法
- 传递性推理算法
- 组合推理算法
- 置信度衰减 + 矛盾检测
- 内置规则注册表
- 单元测试（15+ cases）

### Phase 2: RecallDynamic 接入

- `Store` 新增 `inferenceEngine` 字段
- `RecallDynamic` 调用推理引擎
- 派生事实通过 `Store.LastDerivedFacts()` 暴露给 GUI 层

### Phase 3: System Prompt 注入

- `gui/im_system_prompt.go`：`appendProactiveRecall` 末尾注入推理链
- Token budget 控制（最多 250 token）
- 格式化为自然语言解释

### Phase 4: 诊断与可观测性

- `SemanticGraph.Diagnostics()` 新增推理统计
- TUI memory 面板显示推理链
- 日志记录推理触发和结果

---

## 七、性能预算

| 操作 | 预算 | 说明 |
|------|------|------|
| 推理引擎 `Infer()` | <5ms | 纯内存图遍历，无 LLM 调用 |
| 最大遍历 facts | 200 | 防止大图爆炸 |
| 最大派生 facts | 20 | 控制输出规模 |
| 最大链长 | 3 | 超过 3 跳可靠性不足 |
| System prompt 注入 | ≤250 token | 最多 5 条推理链 |

---

## 八、与现有机制的关系

| 现有机制 | 关系 | 说明 |
|---------|------|------|
| `SemanticGraph.SearchWithOptions` | 数据源 | 推理引擎从 SemanticGraph 的 facts 中读取三元组 |
| `memoryGraph.expand` | 互补 | memoryGraph 做 entry 级关联，推理引擎做 fact 级推导 |
| `EntityIndex.FindRelatedEntities` | 互补 | EntityIndex 做共现统计，推理引擎做逻辑推导 |
| `RecallDynamic` | 消费方 | 推理结果注入 recall 管道 |
| `OnlineExtractor` | 上游 | 提取的三元组是推理的输入数据 |
| `Compressor.dedup` | 无关 | 派生事实不持久化，不参与去重 |

---

## 九、不做的事

1. **不做 LLM 辅助推理**：推理引擎是纯规则的，不调用 LLM。LLM 推理成本高（10-30s）、不确定性大，不适合在 recall 热路径中使用。
2. **不持久化派生事实**：派生事实是 on-demand 计算的，源 facts 变化时自动失效。持久化会引入一致性问题。
3. **不做概率推理**：不引入贝叶斯网络或 Markov Logic Network。规则推理 + 置信度衰减已足够覆盖 MacLaw 的使用场景。
4. **不修改 OnlineExtractor 的提取 prompt**：当前的 entity-relation 提取已经足够。推理质量取决于三元组质量，但提取质量的改进是独立的工作。
5. **不做反向推理（abduction）**：不从结论反推前提。只做正向推理（deduction）。

---

## 十、验收标准

- 查询"张三在哪个城市" + 记忆中有 `(张三, works_at, RapidAI)` 和 `(RapidAI, located_in, 杭州)` → system prompt 中出现推理链"张三 → works_at → RapidAI → located_in → 杭州 ∴ 张三可能在杭州"
- 传递性推理：`A located_in B, B located_in C` → 推导 `A located_in C`（置信度 0.72）
- 矛盾检测：推导结果与已有事实冲突时标注警告
- 跨用户隔离：不同 OwnerID 的 facts 不参与同一条推理链
- 性能：`Infer()` 在 500 facts 的图上 <5ms
- 所有现有 memory 包测试通过
- 15+ 新增推理引擎测试通过
