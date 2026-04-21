# MaClaw 记忆系统技术综述

## 基于 AI Coder 源码的记忆架构分析与相关论文解读

---

## 摘要

本文对 MaClaw（AI Coder / CodeClaw）的记忆系统进行了全面的源码级分析，识别出其中实现的 12 项核心技术，并对每项技术相关的学术论文进行了系统性的检索与综述。MaClaw 的记忆系统是一个工业级的、融合了多种前沿研究思想的混合架构，涵盖了从认知科学启发的遗忘曲线、分层时间记忆树，到信息检索领域的 BM25+向量混合检索、RRF 融合排序，再到 Agent 系统的反思机制、情景-语义转换等广泛技术谱系。

**关键词：** LLM Agent 记忆、TiMem、遗忘曲线、MemGPT、混合检索、知识图谱、对话压缩

---

## 1. 引言

大语言模型（LLM）驱动的 AI Agent 正在从"无状态工具"向"有记忆的长期伙伴"演进。记忆系统是这一转变的核心基础设施——它使 Agent 能够跨会话保留用户偏好、项目知识、交互历史，并据此提供越来越个性化的服务。

MaClaw（内部代号 AI Coder / CodeClaw）是 RapidAI 团队开发的智能编程助手，其记忆系统（`corelib/memory/` 模块）采用了约 40 个 Go 源文件、超过 300KB 的代码量，实现了一套完整的多层记忆架构。

本文的目标是：
1. 从源码中精确识别 MaClaw 记忆系统使用的每一项核心技术
2. 追溯每项技术的学术来源（论文/方法）
3. 对相关论文进行详细解读，形成一份可供工程参考的技术综述

---

## 2. MaClaw 记忆系统架构总览

### 2.1 核心组件一览

MaClaw 的记忆系统由以下核心模块组成：

| 模块 | 源文件 | 功能 |
|------|--------|------|
| Store | store.go | 持久化存储，CRUD 操作，Recall 调度 |
| TemporalTree | temporal_tree.go | 五级时间层次记忆树（TMT） |
| Consolidator | consolidator.go | TiMem 式分层记忆整合（L1-L5） |
| ProfileConsolidator | profile_consolidator.go | L5 增量用户画像整合 |
| Compressor | compressor.go | 记忆压缩、去重、CompactForm 生成 |
| Promoter | promoter.go | 情景记忆 → 语义记忆提升 |
| Reflector | reflector.go | 反思机制（Generative Agents 风格） |
| Forgetting | forgetting.go | Ebbinghaus 遗忘曲线衰减与休眠标记 |
| KnowledgeExtractor | knowledge_extractor.go | 对话后自动知识抽取 |
| BM25Index | bm25.go | 稀疏检索索引 |
| VectorIndex | vector_index.go | 稠密向量检索（余弦相似度） |
| MemoryGraph | graph.go | 记忆关联图谱（BFS 扩展） |
| RecallGating | recall_gating.go | LLM 后检索过滤 |
| QueryExpand | query_expand.go | 查询扩展（实体抽取 + 分词） |
| Pipeline | pipeline.go | 后台维护管线（6小时周期） |
| InjectionScanner | injection_scanner.go | Prompt 注入检测 |
| ContextCompressor | context/compressor.go | 上下文窗口智能压缩 |

### 2.2 数据模型

记忆条目（Entry）包含以下关键字段：

- **Category（分类）**：9 种原始分类 + 4 种 Claude 风格分类（user/feedback/project/reference）
- **Scope（作用域）**：global（全局可见）/ project（项目内可见）
- **Status（状态）**：active / superseded / dormant / archived
- **Strength（强度）**：0.0~1.0+，用于遗忘曲线衰减计算
- **Level（时间层次）**：L1-Segment → L2-Session → L3-Day → L4-Week → L5-Profile
- **Embedding（向量嵌入）**：用于稠密检索
- **RelatedIDs（关联 ID）**：知识图谱边
- **Tags（标签）**：结构化标注
- **CompactForm（压缩形式）**：LLM 生成的精简摘要

### 2.3 Pipeline 工作流

```
decay strengths → compress → promote → reflect → consolidate → profile
      ↓              ↓           ↓          ↓           ↓
  休眠标记       去重+压缩    情景→语义   反思洞察    L2-L5整合    画像更新
```

Pipeline 每 6 小时自动执行一次维护周期，也可手动触发。

---

## 3. 核心技术详解与论文综述

---

### 技术 1 | Ebbinghaus 遗忘曲线（Forgetting Curve）

**源码位置：** `forgetting.go`

**MaClaw 实现：**

MaClaw 采用指数衰减函数模拟艾宾浩斯遗忘曲线：

```
S(t) = S₀ × exp(-λ × hours_elapsed)
```

其中衰减常数 λ = 0.003，对应半衰期约 231 小时（约 9.6 天）。记忆强度低于 0.1 阈值的条目被标记为 dormant（休眠）。当记忆被成功召回时，强度增加 1.0 并重置衰减时间起点——这正是间隔重复（Spaced Repetition）的核心思想。

**核心参数：**
- 衰减速率 λ = 0.003（半衰期 ≈ 9.6 天）
- 休眠阈值 = 0.1
- 召回增强 = +1.0
- 自我身份（self_identity）类记忆永不衰减

**相关论文：**

#### 📄 MemoryBank: Enhancing Large Language Models with Long-Term Memory
- **作者：** Zhong et al.
- **发表：** AAAI 2024
- **链接：** arxiv.org/abs/2305.10250

MemoryBank 是首个将 Ebbinghaus 遗忘曲线系统性地应用于 LLM 记忆系统的工作。该论文提出了一种记忆更新机制，允许 AI 根据时间流逝和记忆的相对重要性来"遗忘"和"强化"记忆，从而提供更类人的记忆体验。

**核心贡献：**
- 将艾宾浩斯遗忘曲线理论引入 LLM 记忆管理
- 根据记忆的重要性和访问频率动态调整记忆强度
- 支持闭源模型（ChatGPT）和开源模型

**与 MaClaw 的关系：** MaClaw 的 `forgetting.go` 直接实现了 MemoryBank 提出的遗忘曲线核心公式，并在其基础上增加了分类保护（self_identity 永不衰减）和批量衰减优化。

#### 📄 Replication and Analysis of Ebbinghaus' Forgetting Curve
- **作者：** Murre & Dros
- **发表：** PLoS ONE