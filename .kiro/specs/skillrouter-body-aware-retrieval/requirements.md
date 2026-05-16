# Requirements Document: SkillRouter Body-Aware Retrieval

## Introduction

基于阿里 SkillRouter 论文的核心发现，对 maclaw 的 tool/skill 检索管线进行重大改进。论文在 8 万 skill 池上的实验表明：skill 的完整实现代码（body）是决定检索准确率的关键信号，而非名称和描述。当前 maclaw 的 `Router` 和 `DynamicToolBuilder` 仅使用 `name + description + tags + synthetic_queries` 构建检索文本，完全缺失 body 信息。

本次改进分四层递进：
1. Body 数据采集与存储（RegisteredTool 扩展）
2. Enrichment Prompt 改进（利用 body 生成更精准的 synthetic queries）
3. Embedding 分叉（BM25 和 embedding 使用不同文本源）
4. LLM Listwise 重排序（可选的 LLM 重排序步骤）

## Glossary

- **Router**: `corelib/tool/router.go` 中的 `Router` 结构体，负责从全量工具中选择最相关的 Top-K 工具发送给 LLM
- **DynamicToolBuilder**: `corelib/tool/builder.go` 中的 `DynamicToolBuilder` 结构体，从 Registry 动态构建工具定义列表
- **RegisteredTool**: `corelib/tool/types.go` 中的工具注册数据结构，描述一个已注册工具的元信息
- **Registry**: `corelib/tool/registry.go` 中的工具注册表，管理所有已注册工具
- **Body**: 工具的完整实现代码或详细参数描述，是 SkillRouter 论文中证明的关键检索信号
- **Body_Summary**: Body 经截断处理后的摘要文本，长度不超过 1500 字符
- **BM25_Index**: `corelib/bm25/bm25.go` 中的稀疏检索索引，基于词频和逆文档频率计算相关性
- **HybridRetriever**: `corelib/tool/hybrid.go` 中的混合检索器，融合 BM25 和向量相似度分数
- **EnrichmentStore**: `corelib/tool/enrichment.go` 中的增强存储，管理工具的 synthetic queries
- **Embedder**: `corelib/embedding/embedder.go` 中的嵌入接口，生成文本的稠密向量表示
- **Listwise_Reranker**: 使用 LLM 对候选工具列表进行整体重排序的组件，一次性输出排序结果
- **LLMCaller**: 抽象的 LLM 调用接口，用于重排序器注入
- **Fused_Score**: BM25 归一化分数与向量余弦相似度的加权融合分数
- **MaxToolBudget**: Router 发送给 LLM 的最大工具数量上限（当前为 28）
- **NL_Skill**: 自然语言技能，以 SKILL.md 格式定义的自动化技能
- **MCP_Tool**: 通过 MCP 协议暴露的外部工具
- **Builtin_Tool**: maclaw 内置的静态工具（如 bash、read_file 等）

## Requirements

### Requirement 1: RegisteredTool Body 字段扩展

**User Story:** As a tool routing system, I want each RegisteredTool to carry its implementation body, so that the retrieval pipeline can leverage body content as a high-value signal for accurate tool selection.

#### Acceptance Criteria

1. THE RegisteredTool SHALL include a `Body` field of type `string` that stores the tool's implementation code or detailed parameter description.
2. THE RegisteredTool SHALL include a `BodySummary` field of type `string` that stores the truncated version of Body for use in retrieval.
3. WHEN a RegisteredTool's Body exceeds 1500 characters, THE Registry SHALL truncate Body into BodySummary using a markdown-aware truncation strategy that prioritizes headings, parameter lists, and code blocks.
4. WHEN a RegisteredTool's Body is 1500 characters or fewer, THE Registry SHALL set BodySummary equal to Body without modification.
5. WHEN a RegisteredTool has an empty Body, THE Registry SHALL set BodySummary to an empty string.
6. THE TruncateBody function SHALL preserve complete markdown headings, parameter list items, and code block boundaries rather than cutting mid-line.

### Requirement 2: NL Skill Body 采集

**User Story:** As a skill author, I want the system to automatically extract SKILL.md content as the skill's body, so that the retrieval pipeline can understand the skill's full implementation.

#### Acceptance Criteria

1. WHEN an NL_Skill is registered from a SKILL.md file, THE Skill_Registration_Flow SHALL read the SKILL.md content and populate the RegisteredTool's Body field with the markdown content.
2. WHEN the SKILL.md file cannot be read, THE Skill_Registration_Flow SHALL log a warning and leave the Body field empty.
3. WHEN an NL_Skill is imported from a remote source with AgentSkillMD content, THE Skill_Registration_Flow SHALL use the AgentSkillMD content as the Body field.

### Requirement 3: MCP Tool Body 采集

**User Story:** As an MCP tool provider, I want the system to construct a body from the tool's input schema and parameter descriptions, so that MCP tools participate in body-aware retrieval.

#### Acceptance Criteria

1. WHEN an MCP_Tool is registered, THE MCP_Registration_Flow SHALL construct the Body field by serializing the tool's inputSchema JSON and concatenating parameter descriptions.
2. WHEN an MCP_Tool has no inputSchema or an empty inputSchema, THE MCP_Registration_Flow SHALL leave the Body field empty.
3. THE MCP_Registration_Flow SHALL format the Body as a readable text block containing parameter names, types, and descriptions extracted from the inputSchema.

### Requirement 4: Builtin Tool Body 采集

**User Story:** As a system maintainer, I want builtin tools to have hardcoded body content describing their parameter schemas and typical usage, so that builtin tools also benefit from body-aware retrieval.

#### Acceptance Criteria

1. THE Builtin_Tool_Registration SHALL provide a `BuiltinBodies` map that maps builtin tool names to their body text content.
2. WHEN a Builtin_Tool is registered, THE Registration_Flow SHALL look up the tool name in `BuiltinBodies` and populate the Body field with the corresponding content.
3. WHEN a Builtin_Tool has no entry in `BuiltinBodies`, THE Registration_Flow SHALL leave the Body field empty.
4. THE BuiltinBodies entries SHALL contain the tool's parameter schema description and typical usage patterns in a concise text format.

### Requirement 5: Enrichment Prompt 改进

**User Story:** As a retrieval pipeline, I want the enrichment prompt to include body summary information, so that LLM-generated synthetic queries reflect implementation details and produce more discriminative queries.

#### Acceptance Criteria

1. WHEN GenerateEnrichmentPrompt is called, THE EnrichmentStore SHALL include the tool's BodySummary in the prompt input alongside name and description.
2. WHEN the BodySummary is empty, THE GenerateEnrichmentPrompt function SHALL fall back to using only name and description as prompt input.
3. THE GenerateEnrichmentPrompt system prompt SHALL instruct the LLM to generate queries that reflect implementation-level details visible in the body summary.
4. THE GenerateEnrichmentPrompt system prompt SHALL instruct the LLM to generate queries that distinguish the tool from similar tools in the same category.

### Requirement 6: BM25 文本构建保持不变

**User Story:** As a retrieval pipeline, I want BM25 search text to remain unchanged (name + description + tags + synthetic queries), so that adding body content does not inflate average document length and dilute short query matching.

#### Acceptance Criteria

1. THE Router SHALL continue to use `buildSearchText()` for BM25 indexing, which concatenates name, description, tags, and synthetic queries.
2. THE Router SHALL NOT include Body or BodySummary in the text passed to the BM25_Index.
3. THE DynamicToolBuilder SHALL continue to use the same BM25 text construction logic as the Router.

### Requirement 7: Embedding 专用文本构建

**User Story:** As a retrieval pipeline, I want a separate text construction function for embedding that includes body summary, so that the dense vector retrieval benefits from the richer semantic context of implementation code.

#### Acceptance Criteria

1. THE Router SHALL provide a `buildEmbeddingText()` function that concatenates name, description, and BodySummary for each candidate tool.
2. WHEN BodySummary is empty, THE `buildEmbeddingText()` function SHALL fall back to using name and description only.
3. THE Router SHALL pass the embedding text map (from `buildEmbeddingText()`) to `HybridRetriever.FuseScores()` instead of the BM25 text map.
4. THE DynamicToolBuilder SHALL use the same `buildEmbeddingText()` logic when constructing text for `HybridRetriever.FuseScores()`.
5. THE ToolEmbeddingCache SHALL automatically invalidate cached embeddings when the embedding text changes, via its existing SHA-256 key mechanism.

### Requirement 8: LLM Listwise 重排序

**User Story:** As a retrieval pipeline, I want an optional LLM-based listwise reranking step after score fusion, so that the final tool selection benefits from the LLM's deep understanding of query-tool relevance.

#### Acceptance Criteria

1. WHEN the Listwise_Reranker is configured (via `Router.SetReranker()`) and the number of candidates exceeds MaxToolBudget, THE Router SHALL invoke the Listwise_Reranker after fused score sorting.
2. THE Listwise_Reranker SHALL take the top 20 candidates from fused score sorting and produce a reranked list of top 5 candidates.
3. THE Listwise_Reranker prompt SHALL include the user message and each candidate's name plus BodySummary.
4. WHEN the Listwise_Reranker is not configured (SetReranker not called), THE Router SHALL skip the reranking step and use fused scores directly.
5. IF the Listwise_Reranker invocation fails or returns an error, THEN THE Router SHALL fall back to the fused score ordering and log a warning.
6. IF the Listwise_Reranker returns fewer than 5 results, THEN THE Router SHALL supplement with the next highest-scoring candidates from the fused score list.
7. THE Listwise_Reranker SHALL use a single LLM call with all 20 candidates presented as a numbered list, requesting the LLM to output the top 5 indices in order of relevance.

### Requirement 9: LLMCaller 接口与注入

**User Story:** As a system architect, I want the reranker to depend on an abstract LLM caller interface, so that the Router remains decoupled from specific LLM implementations.

#### Acceptance Criteria

1. THE Router SHALL define a `Reranker` interface with a single method for listwise reranking that accepts a user message and a list of candidate tool summaries, and returns an ordered list of tool names.
2. THE Router SHALL provide a `SetReranker()` method that accepts a Reranker implementation and stores it for use during Route calls.
3. WHEN SetReranker is called with nil, THE Router SHALL disable the reranking step.
4. THE DynamicToolBuilder SHALL provide the same `SetReranker()` method with identical behavior.

### Requirement 10: 向后兼容性

**User Story:** As an existing user, I want the system to behave identically to the current version when no body data is available, so that the upgrade does not break existing functionality.

#### Acceptance Criteria

1. WHEN a RegisteredTool has an empty Body and empty BodySummary, THE Router SHALL produce identical tool selection results as the current implementation.
2. WHEN the Embedder is a NoopEmbedder, THE Router SHALL use only BM25 scores for ranking, identical to the current behavior.
3. WHEN the Reranker is not configured, THE Router SHALL skip the reranking step and use the existing three-signal scoring formula.
4. THE Router SHALL maintain the existing three-signal scoring formula (retrieval + experience + priority) with the same weights (α=0.6, β=0.3, γ=0.1).
5. THE DynamicToolBuilder SHALL maintain identical backward compatibility guarantees as the Router.

### Requirement 11: Body 截断策略

**User Story:** As a retrieval pipeline, I want a well-defined truncation strategy for body content, so that the embedding model's context window is respected while preserving the most informative content.

#### Acceptance Criteria

1. THE TruncateBody function SHALL accept a raw body string and a maximum character limit (default 1500).
2. THE TruncateBody function SHALL preserve complete lines rather than cutting mid-line.
3. THE TruncateBody function SHALL prioritize retaining markdown headings (lines starting with `#`), parameter list items (lines starting with `-` or `*`), and code block content (lines between ``` markers).
4. WHEN the body is within the character limit, THE TruncateBody function SHALL return the body unchanged.
5. WHEN truncation is necessary, THE TruncateBody function SHALL append an ellipsis marker `...` to indicate truncation occurred.

### Requirement 12: 可观测性与日志

**User Story:** As a system operator, I want detailed logging of the body-aware retrieval pipeline, so that I can diagnose retrieval quality issues and tune parameters.

#### Acceptance Criteria

1. WHEN hybrid retrieval is active and body-aware embedding text is used, THE Router SHALL log the top-5 candidates with their BM25 score, vector score, and fused score to the route log file.
2. WHEN the Listwise_Reranker is invoked, THE Router SHALL log the reranker input (candidate count) and output (reranked order) to the route log file.
3. IF the Listwise_Reranker fails, THEN THE Router SHALL log the error reason and the fallback action to the route log file.
4. THE writeRouteLog function SHALL include a `body_aware` boolean field indicating whether body-enhanced embedding text was used for the current route call.
