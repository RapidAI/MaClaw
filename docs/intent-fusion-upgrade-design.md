# MacLaw 意图理解机制升级设计：借鉴 intent-fusion 双通道融合架构

## 1. 对比分析

### 1.1 intent-fusion 核心架构

[intent-fusion](https://github.com/Liyuan1992/intent-fusion) 是一个经过生产验证的双通道意图识别库，在 57 个意图、7 个领域、140 个标注用例上达到 97.1% 准确率（单模型最高 86.5%）。

核心设计：

| 层级 | 通道 | 延迟 | 作用 |
|------|------|------|------|
| Channel 1 | Embedding Recall | 5-15ms | 快速词汇/表面相似度匹配，top-k 候选召回 |
| Channel 2 | Intent Tree (LLM) | 200-800ms | 深度语义推理，层级化 CoT 推理 |
| Merge | 加权融合 | <1ms | `final = α·emb + (1-α)·tree`，三态判决 |

关键设计决策：
- **双文本分离**：每个意图有独立的 `embed_text`（关键词密集、同义词丰富）和 `tree_text`（描述性、消歧提示），因为单一描述无法同时优化词汇匹配和语义推理
- **三态判决**：CLEAR（直接路由）/ AMBIGUOUS（执行 top，建议 runner-up）/ LOW（降级到自由对话）
- **单通道降级**：任一通道失败时另一通道仍可工作（α 动态调整为 0 或 1）
- **校准机制**：通过 grid search 在标注数据上找到最优 α 和 δ 参数
- **Intent Tree**：将意图按 domain 分组构建树形 prompt，LLM 做层级推理（先定位 domain，再定位 intent）

### 1.2 MacLaw 当前架构

MacLaw 的意图理解分散在多个模块中，形成了一个 **四层 + 多分支** 的复杂系统：

| 层级 | 模块 | 延迟 | 作用 |
|------|------|------|------|
| Layer 1 | QuickFilter | <1ms | 快速拒绝（闲聊、活跃会话） |
| Layer 1 | UIC KeywordRegistry | <1ms | 关键词匹配，12 个 IntentLabel |
| Layer 2 | UIC Embedding (cosine) | ~5ms | anchor 向量余弦相似度 |
| Layer 3 | UIC LLM | ~8s | LLM 分类 12 个标签 |
| 并行 | WorkflowRegistry Keywords | <1ms | 模板关键词匹配（强/弱两层） |
| 并行 | WorkflowRegistry BM25 | ~5ms | 模板描述 BM25 语义检索 |
| 并行 | IntentUnderstandingManager | 10-30s | 多轮 LLM 对话确认工作流类型 |
| 并行 | CodingToolGate | <1ms | 编码意图分类 + 工具拦截 |
| 并行 | conditionalKeepRules | <1ms | 条件工具激活（SSH/Browser/Search） |

### 1.3 核心差异

| 维度 | intent-fusion | MacLaw 当前 |
|------|--------------|-------------|
| **架构** | 统一双通道并行 + 融合 | 多模块串行/并行混合，各自独立决策 |
| **意图定义** | IntentEntry（embed_text + tree_text） | 分散在 KeywordRegistry、intentAnchors、LLM prompt 中 |
| **融合机制** | 加权公式 `α·emb + (1-α)·tree` | 逐层升级（L1 不确定→L2 不确定→L3），无跨通道融合 |
| **判决模型** | 三态（CLEAR/AMBIGUOUS/LOW） | 二态（confident/escalate），最终取最高层结果 |
| **校准** | 离线 grid search 找最优参数 | 硬编码阈值（0.78/0.10/0.90 等） |
| **降级** | 单通道降级（α 动态调整） | 逐层 fallback（L3 失败用 L2，L2 失败用 L1） |
| **工具激活** | 不涉及 | ToolAffinity + conditionalKeepRules |

### 1.4 MacLaw 当前问题

1. **无跨通道融合**：L1 关键词和 L2 embedding 的结果不融合。L1 confident 就直接返回，即使 L2 可能给出不同答案。intent-fusion 的核心洞察是：embedding 擅长表面匹配，LLM 擅长语义推理，两者互补而非替代。

2. **意图定义分散**：同一个意图（如 SSH）的关键词在 `KeywordRegistry`、`sshKeywords`（im_intent_classifier.go）、`conditionalKeepRules`（router.go）中各有一份，维护成本高且容易不一致。

3. **工作流意图和工具意图割裂**：`QuickFilter` + `IntentUnderstandingManager` 负责工作流分类（coding/product_design/...），`UnifiedIntentClassifier` 负责工具意图分类（ssh/browser/search/...），两者独立运行，没有信息共享。用户说"开发一个网站并部署到服务器"时，工作流层看到 coding，工具层看到 ssh，但两者不知道对方的判断。

4. **硬编码阈值无校准**：L2 的 confident 阈值（top1 >= 0.78 && gap >= 0.10）和 L1 的 confident 阈值（confidence >= 0.90）都是手动设定的，没有在标注数据上校准。

5. **LLM 通道效率低**：UIC 的 Layer 3 和 IntentUnderstandingManager 的 LLM 调用是串行的（先 UIC L3 判断工具意图，再 IUM 判断工作流类型），两次 LLM 调用可以合并。

---

## 2. 升级设计

### 2.1 设计原则

1. **渐进式升级**：不推翻现有架构，在 `corelib/intent` 包内增强，保持所有现有消费者（gui/coding_tool_gate、gui/im_message_handler、corelib/tool/router）的接口不变
2. **借鉴融合而非照搬**：intent-fusion 是 Python 库，面向 SaaS 场景的 57 个意图。MacLaw 是 Go 桌面应用，面向 12 个工具意图 + 19 个工作流模板。直接移植不合适，但融合思想和三态判决值得借鉴
3. **统一意图定义**：将分散的意图描述收敛到单一数据结构，类似 IntentEntry 的 embed_text/tree_text 分离
4. **保持向后兼容**：所有现有 adapter（ToTaskIntent、ToGateIntent）继续工作

### 2.2 统一意图条目（Unified Intent Entry）

借鉴 intent-fusion 的 `IntentEntry` 双文本设计，在 `corelib/intent` 中新增：

```go
// IntentDefinition is the single source of truth for one intent category.
// Inspired by intent-fusion's IntentEntry dual-text design: a single
// description cannot simultaneously optimize for lexical matching and
// semantic reasoning.
type IntentDefinition struct {
    Label       IntentLabel
    Domain      string   // grouping for tree reasoning (e.g., "Coding", "Remote", "Content")
    Keywords    []KeywordEntry // Layer 1: keyword rules (existing)
    EmbedTexts  []string       // Layer 2: anchor sentences for embedding cosine
    TreeText    string         // Layer 3: descriptive text for LLM tree reasoning
    ToolNames   []string       // tool affinity: tools to activate when this intent wins
}
```

这将 `KeywordRegistry`、`intentAnchor`、`ToolAffinityRegistry`、LLM system prompt 中的意图描述统一到一个结构中。新增模板或意图时只需添加一个 `IntentDefinition`，所有层自动生效。

### 2.3 双通道并行融合（Dual-Channel Fusion）

改造 `UnifiedIntentClassifier.Classify()` 的执行模型：

**当前**（串行升级）：
```
L1 keyword → confident? → return
                ↓ no
L2 embedding → confident? → return
                ↓ no
L3 LLM → return
```

**升级后**（并行融合）：
```
L1 keyword → confident? → return (fast path, <1ms)
                ↓ no
┌─────────────────────────────────────┐
│         parallel execution          │
│                                     │
│  L2 embedding (5ms)  │  L3 LLM tree reasoning (2-8s)  │
│  cosine similarity   │  domain-grouped CoT             │
└──────────┬───────────┴──────────┬───────────────────────┘
           │                      │
           └──────────┬───────────┘
                      │
              ┌───────▼────────┐
              │  Fuse & Decide │
              │  final = α·emb + (1-α)·tree  │
              │  CLEAR / AMBIGUOUS / LOW      │
              └───────┬────────┘
                      │
              ┌───────▼────────┐
              │  Map to Result │
              │  ClassificationResult + ToolNames  │
              └────────────────┘
```

关键变化：
- L2 和 L3 **并行执行**（Go goroutine + channel），总延迟 = max(L2, L3) 而非 L2 + L3
- 两个通道的结果通过加权公式融合，而非"L2 不确定才用 L3"
- 引入三态判决（CLEAR/AMBIGUOUS/LOW），替代当前的二态（confident/escalate）
- L3 从"分类 12 个标签"改为"Intent Tree 层级推理"（借鉴 intent-fusion 的 tree.py）

### 2.4 Intent Tree 推理（Layer 3 升级）

当前 L3 的 system prompt 是一个扁平的 12 标签列表。借鉴 intent-fusion 的 Intent Tree 设计，改为 domain 分组的层级结构：

```
── 编码开发 (Coding) ──
  coding: 用户要从零创建软件/应用/游戏/工具，需要完整开发流程
  bug_fix: 用户要修复已有代码的 bug、调试崩溃、排查错误
  maintenance: 用户要重构/优化/升级已有代码，不添加新功能

── 远程操作 (Remote) ──
  ssh: 用户要连接远程服务器、执行命令、查看日志、管理服务

── 浏览器自动化 (Browser) ──
  browser: 用户要自动化浏览器操作：导航网页、点击元素、填写表单、录制回放

── 内容处理 (Content) ──
  non_coding: 用户要翻译/总结/整理/搜索/写文档等非编码任务
  search: 用户要在网上搜索信息、文档、解决方案
  document_delivery: 用户要打开/发送/导出文件
  office: 用户要创建/操作 PPT/Excel/Word 文档

── 特殊 (Special) ──
  continuation: 用户用短语表示继续/开始之前讨论的任务
  ambiguous: 消息不明确，可能属于多个类别
  unknown: 不属于任何已知类别
```

LLM 先推理 domain，再在 domain 内选择 intent，减少搜索空间，提高准确率。

### 2.5 三态判决与下游映射

```go
type FusionVerdict string

const (
    VerdictClear     FusionVerdict = "clear"     // top 候选明确占优 → 直接路由
    VerdictAmbiguous FusionVerdict = "ambiguous"  // top-2 接近 → 执行 top，提示 runner-up
    VerdictLow       FusionVerdict = "low"        // 无置信匹配 → 降级到自由对话
)

type FusionResult struct {
    Verdict    FusionVerdict
    Top        FusedCandidate  // 最佳候选
    RunnerUp   *FusedCandidate // 次佳候选（AMBIGUOUS 时非 nil）
    EmbMs      float64         // embedding 通道耗时
    TreeMs     float64         // tree 通道耗时
    TotalMs    float64         // 总耗时
    Degraded   bool            // 单通道降级
}

type FusedCandidate struct {
    Label      IntentLabel
    FinalScore float64
    EmbScore   float64
    TreeScore  float64
    InEmb      bool
    InTree     bool
}
```

下游映射（保持向后兼容）：
- `FusionResult` → `ClassificationResult`：Top.Label → Primary，Top.FinalScore → Confidence
- AMBIGUOUS 时 RunnerUp.Label → Secondary[0]
- LOW 时 Primary = LabelAmbiguous，Confidence = Top.FinalScore

### 2.6 工作流意图融合

当前 `QuickFilter` + `IntentUnderstandingManager` 与 `UnifiedIntentClassifier` 完全独立。升级后：

1. **UIC 新增工作流标签**：在 IntentLabel 中新增 `LabelWorkflow IntentLabel = "workflow"`
2. **Intent Tree 新增工作流 domain**：
```
── 工作流任务 (Workflow) ──
  workflow: 用户要启动多阶段工作流（编码开发、产品设计、商业计划等），
           需要经过需求→设计→执行的完整流程。
           区别于 coding（直接编码）：workflow 强调多阶段文档产出和用户确认。
```
3. **融合结果中携带工作流信号**：当 Top.Label == LabelWorkflow 时，`ClassificationResult` 新增 `WorkflowHint string` 字段，携带 LLM 推理出的工作流类型（coding/product_design/...）
4. **QuickFilter 消费 UIC 结果**：QuickFilter 在 `FilterNeedsUnderstanding` 之前先查询 UIC 缓存，如果 UIC 已经判定为 LabelWorkflow 且 confidence >= 0.80，直接返回 `FilterNeedsUnderstanding` 并携带 WorkflowHint，跳过 BM25 检索

这样 UIC 的 L2 embedding（5ms）可以提前给出工作流信号，减少 IntentUnderstandingManager 的 LLM 调用（10-30s）的等待时间。

### 2.7 校准机制

借鉴 intent-fusion 的 calibration.py，在 `corelib/intent` 中新增离线校准工具：

```go
// calibration.go
type CalibrationCase struct {
    Message        string
    ExpectedLabel  IntentLabel
    Note           string
}

type CalibrationResult struct {
    Alpha    float64
    Delta    float64
    Accuracy float64
    Correct  int
    Total    int
}

func RunGridSearch(cases []CalibrationCase, defs []IntentDefinition, 
    embedder embedding.Embedder, llmFunc LLMClassifyFunc) CalibrationResult
```

标注数据来源：
- 从 maclaw-improvements.md 中提取的 50+ 个真实用例（PPT 文件操作 vs PPT 设计、"开工"上下文、browser 误激活等）
- 从 intent_understanding.go 的易混淆示例中提取 30+ 个用例
- 从 coding_tool_gate_test.go 中提取 20+ 个用例

### 2.8 单通道降级

与 intent-fusion 一致：
- **L3 LLM 失败**（超时/网络错误）：α 强制为 1.0，仅用 L2 embedding 结果
- **L2 embedding 失败**（模型未加载/warmup 未完成）：α 强制为 0.0，仅用 L3 LLM 结果
- **两者都失败**：回退到 L1 keyword 结果
- `FusionResult.Degraded = true` 通知下游

---

## 3. 实现计划

### Phase 1: 统一意图定义 + Intent Tree prompt ✅ 已完成

**文件变更**：
- `corelib/intent/fusion_types.go`：新增 `IntentDefinition`、`FusionVerdict`、`FusionResult`、`FusedCandidate`、`FusionConfig`
- `corelib/intent/definitions.go`：新文件，统一定义 12 个 IntentLabel 的 Domain/EmbedTexts/TreeText/ToolNames；`PopulateKeywords()` 从 `defaultKeywords` 自动填充 Keywords；`FullDefinitions()` 返回完整定义；`NewKeywordRegistryFromDefinitions()` 和 `NewToolAffinityRegistryFromDefinitions()` 从定义构建
- `corelib/intent/tree.go`：新文件，`BuildIntentTreeText()` 按 domain 分组构建树形 prompt，`ParseTreeResponse()` 解析 LLM 的 `<think>` + JSON 响应，`ClassifyByTree()` 执行单次 LLM 调用
- `corelib/intent/fusion.go`：新文件，`MergeAndScore()` 加权融合算法，`Decide()` 三态判决
- `corelib/intent/keyword_registry.go`：提取 `newKeywordRegistryFromEntries()` 共享构建逻辑

### Phase 2: 并行融合 + 三态判决 ✅ 已完成

**文件变更**：
- `corelib/intent/classifier.go`：`Classify()` 改为并行执行 L2+L3 并融合；新增 `classifyWithFusion()`、`embeddingTopK()`、`fusionToClassification()`；`New()` 改为使用 definitions-derived 构造器
- `corelib/intent/classifier_fusion_test.go`：7 个融合路径测试
- `corelib/intent/definitions_test.go`：4 个 round-trip 等价性测试

### Phase 3: 工作流意图融合（待实施）

**文件变更**：
- `corelib/intent/types.go`：新增 `LabelWorkflow`
- `corelib/intent/definitions.go`：新增 workflow IntentDefinition
- `corelib/workflow/quick_filter.go`：消费 UIC 缓存结果
- `gui/im_message_handler_workflow.go`：使用 UIC 的 WorkflowHint 加速工作流启动

### Phase 4: 校准工具 ✅ 已完成

**文件变更**：
- `corelib/intent/calibration.go`：`RunGridSearch()` grid search 实现，`FormatReport()` 报告格式化，支持 embedding-only / tree-only / dual-channel 三种模式
- `corelib/intent/calibration_cases.go`：74 个标注用例，从 maclaw-improvements.md 真实 bug 报告和 intent_understanding.go 示例中提取，覆盖全部 10 个主要 IntentLabel
- `corelib/intent/calibration_test.go`：5 个测试（keyword-only 校准、dual-channel 校准、空 scorer、报告格式化、用例覆盖率）

---

## 4. 预期收益

| 指标 | 当前 | 升级后 |
|------|------|--------|
| 意图分类准确率 | ~85%（估计，无标注数据） | ~95%+（基于 intent-fusion 的生产数据） |
| Browser 误激活率 | 需要多层防护（#34, #45, #45.1） | 融合判决自然消除弱信号误匹配 |
| 工作流启动延迟 | 10-30s（等 IUM LLM） | 5-8s（UIC L2 提前给出信号） |
| 意图定义维护成本 | 5+ 文件分散维护 | 1 个 definitions.go 统一维护 |
| 新增意图/模板 | 改 3-5 个文件 | 改 1 个文件（IntentDefinition） |

## 5. 风险与缓解

1. **L3 LLM 延迟增加**：Intent Tree prompt 比当前扁平 prompt 更长。缓解：tree_text 精简，控制在 800 token 以内；并行执行不增加总延迟
2. **融合参数敏感**：α 和 δ 选择不当可能降低准确率。缓解：Phase 4 校准工具 + 标注数据回归测试
3. **向后兼容风险**：现有 20+ 个测试依赖 ClassificationResult 的具体值。缓解：adapter 层保持接口不变，融合结果映射到现有类型
