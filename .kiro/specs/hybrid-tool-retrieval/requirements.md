# Requirements Document

## Introduction

MaClaw 的工具路由系统（`Router.Route()` 和 `DynamicToolBuilder.Build()`）当前仅使用 BM25 文本匹配对候选工具进行评分和筛选。项目已有一个可用的 Gemma2 300M GGUF 嵌入模型（`corelib/embedding/`），通过 `Embedder` 接口提供稠密向量嵌入，但目前仅用于记忆存储的向量搜索。

本需求旨在将向量相似度集成到工具路由管线中，构建完整的混合检索系统（BM25 + 向量余弦相似度 + 加权融合），在嵌入器可用时启用混合检索，不可用时自动回退到纯 BM25。

## Glossary

- **Hybrid_Retriever**: 混合检索器，组合 BM25 稀疏检索和向量稠密检索的评分融合组件
- **Router**: `corelib/tool/router.go` 中的 `Router` 结构体，负责根据用户消息从全部工具中选择最相关的子集发送给 LLM
- **DynamicToolBuilder**: `corelib/tool/builder.go` 中的 `DynamicToolBuilder` 结构体，负责从 Registry 动态构建工具定义并按相关性过滤
- **Embedder**: `corelib/embedding/embedder.go` 中定义的接口，提供 `Embed(string) ([]float32, error)` 和 `EmbedBatch([]string) ([][]float32, error)` 方法
- **NoopEmbedder**: 嵌入器不可用时的空实现，`Embed()` 返回 `nil, nil`，`Dim()` 返回 0
- **BM25_Index**: `corelib/bm25/bm25.go` 中的稀疏检索索引，基于 gse 中文分词 + BM25 评分
- **Cosine_Similarity**: 两个向量之间的余弦相似度，值域 [-1, 1]，用于衡量语义相似性
- **Tool_Embedding_Cache**: 工具描述文本的向量嵌入缓存，避免每次路由请求重复计算
- **Fusion_Weight**: BM25 分数和向量相似度分数的融合权重参数，控制两种信号的相对重要性
- **Score_Normalization**: 将 BM25 和向量相似度分数归一化到可比较范围的过程

## Requirements

### Requirement 1: 混合检索器核心组件

**User Story:** 作为 MaClaw 开发者，我希望有一个独立的混合检索器组件，以便在工具路由中组合 BM25 和向量相似度评分。

#### Acceptance Criteria

1. THE Hybrid_Retriever SHALL accept an Embedder instance and a BM25_Index instance as dependencies
2. WHEN the Embedder is a NoopEmbedder, THE Hybrid_Retriever SHALL use only BM25_Index scores for ranking
3. WHEN the Embedder is functional (not NoopEmbedder), THE Hybrid_Retriever SHALL compute both BM25 scores and vector Cosine_Similarity scores for each candidate tool
4. THE Hybrid_Retriever SHALL normalize BM25 scores using min-max normalization across the candidate set before fusion
5. THE Hybrid_Retriever SHALL combine normalized BM25 scores and Cosine_Similarity scores using the formula: `final_score = alpha * normalized_bm25 + (1 - alpha) * cosine_similarity`, where alpha is the Fusion_Weight
6. THE Hybrid_Retriever SHALL use a default Fusion_Weight of 0.6 (60% BM25, 40% vector)
7. THE Hybrid_Retriever SHALL return a map of tool ID to fused score, consistent with the existing BM25_Index.Score() return type

### Requirement 2: 工具嵌入缓存

**User Story:** 作为 MaClaw 开发者，我希望工具描述的向量嵌入被缓存，以避免每次路由请求都重新计算嵌入。

#### Acceptance Criteria

1. THE Tool_Embedding_Cache SHALL store precomputed embedding vectors keyed by tool description text
2. WHEN a tool's description text has not changed since the last computation, THE Tool_Embedding_Cache SHALL return the cached embedding vector without calling the Embedder
3. WHEN a tool's description text changes or a new tool is added, THE Tool_Embedding_Cache SHALL compute and store the new embedding vector
4. THE Tool_Embedding_Cache SHALL be safe for concurrent read access from multiple goroutines
5. WHEN the Embedder returns an error for a specific tool description, THE Tool_Embedding_Cache SHALL skip that tool's vector score and use only the BM25 score for that tool

### Requirement 3: Router 集成混合检索

**User Story:** 作为 MaClaw 用户，我希望工具路由在嵌入器可用时自动使用混合检索，以获得更准确的工具推荐。

#### Acceptance Criteria

1. THE Router SHALL accept an optional Embedder via a `SetEmbedder(Embedder)` method
2. WHEN an Embedder is set and is not a NoopEmbedder, THE Router SHALL use the Hybrid_Retriever to score candidate tools instead of using BM25_Index alone
3. WHEN no Embedder is set or the Embedder is a NoopEmbedder, THE Router SHALL continue to use only BM25_Index for scoring (existing behavior preserved)
4. THE Router SHALL embed the user message query once per Route() call and reuse the resulting vector for all candidate comparisons
5. IF the Embedder returns an error when embedding the user message query, THEN THE Router SHALL fall back to pure BM25 scoring for that Route() call

### Requirement 4: DynamicToolBuilder 集成混合检索

**User Story:** 作为 MaClaw 用户，我希望动态工具构建器在过滤工具时也使用混合检索，以保持与 Router 一致的检索质量。

#### Acceptance Criteria

1. THE DynamicToolBuilder SHALL accept an optional Embedder via a `SetEmbedder(Embedder)` method
2. WHEN an Embedder is set and is not a NoopEmbedder, THE DynamicToolBuilder SHALL use the Hybrid_Retriever to score dynamic tools in the Build() method
3. WHEN no Embedder is set or the Embedder is a NoopEmbedder, THE DynamicToolBuilder SHALL continue to use only BM25_Index for scoring (existing behavior preserved)
4. THE DynamicToolBuilder SHALL share the same Tool_Embedding_Cache instance with the Router when both are configured with the same Embedder

### Requirement 5: GUI 层嵌入器注入

**User Story:** 作为 MaClaw 桌面用户，我希望当向量搜索功能启用且嵌入模型已加载时，工具路由自动升级为混合检索模式。

#### Acceptance Criteria

1. WHEN the vector search feature is enabled and the Embedder is successfully loaded, THE GUI layer SHALL inject the Embedder into both the ToolRouter and the DynamicToolBuilder
2. WHEN the vector search feature is disabled or the Embedder fails to load, THE GUI layer SHALL inject a NoopEmbedder (or not inject any Embedder), ensuring pure BM25 fallback
3. WHEN the user toggles the vector search setting at runtime, THE GUI layer SHALL update the Embedder in the Router and DynamicToolBuilder accordingly without requiring application restart
4. THE GUI layer SHALL reuse the same Embedder instance that is used for memory store vector search, avoiding duplicate model loading

### Requirement 6: 余弦相似度计算

**User Story:** 作为 MaClaw 开发者，我希望有一个高效的余弦相似度计算函数，用于向量检索评分。

#### Acceptance Criteria

1. THE Cosine_Similarity function SHALL compute the cosine similarity between two float32 vectors of equal dimension
2. IF either input vector is nil or has zero length, THEN THE Cosine_Similarity function SHALL return 0.0
3. IF the two input vectors have different lengths, THEN THE Cosine_Similarity function SHALL return 0.0
4. IF either input vector has zero magnitude, THEN THE Cosine_Similarity function SHALL return 0.0 (avoiding division by zero)
5. FOR ALL pairs of identical non-zero vectors, THE Cosine_Similarity function SHALL return 1.0

### Requirement 7: 查询嵌入缓存

**User Story:** 作为 MaClaw 开发者，我希望频繁出现的用户查询的嵌入向量被短期缓存，以减少重复的嵌入计算开销。

#### Acceptance Criteria

1. THE Router SHALL maintain a short-lived cache (LRU or time-based) for user query embedding vectors
2. WHEN the same user query text is received within the cache TTL, THE Router SHALL reuse the cached query embedding instead of calling the Embedder again
3. THE query embedding cache SHALL have a maximum capacity of 64 entries to bound memory usage
4. THE query embedding cache SHALL have a default TTL of 30 seconds
5. WHEN the cache reaches maximum capacity, THE Router SHALL evict the least recently used entry

### Requirement 8: 混合检索的可观测性

**User Story:** 作为 MaClaw 开发者，我希望能够观察混合检索的运行状态，以便调试和优化检索质量。

#### Acceptance Criteria

1. WHEN hybrid retrieval is active, THE Router SHALL log (at debug level) the top-5 tools with their BM25 score, vector score, and fused score for each Route() call
2. THE VectorSearchStatus struct SHALL include a new field indicating whether hybrid tool retrieval is active (distinct from memory vector search)
3. WHEN the Embedder encounters an error during a Route() call, THE Router SHALL log the error at warning level and continue with BM25 fallback
