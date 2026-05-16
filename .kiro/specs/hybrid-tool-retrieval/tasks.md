# Implementation Plan: Hybrid Tool Retrieval

## Overview

将 BM25 稀疏检索与 Gemma2 嵌入模型的稠密向量检索相结合，构建混合检索系统。实现顺序：先构建底层工具函数和缓存组件，再组装 HybridRetriever，然后集成到 Router/Builder，最后接入 GUI 层。

## Tasks

- [x] 1. Implement CosineSimilarity and ToolEmbeddingCache
  - [x] 1.1 Create `corelib/tool/hybrid.go` with `CosineSimilarity` function
    - Implement cosine similarity between two `[]float32` vectors
    - Return 0.0 for nil, empty, mismatched-length, or zero-magnitude inputs
    - Return `dot(a,b) / (|a| * |b|)` for valid inputs
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

  - [ ]* 1.2 Write property test for CosineSimilarity
    - **Property 5: Cosine similarity correctness**
    - Use `rapid` to generate random float32 vector pairs
    - Verify `CosineSimilarity(v, v) == 1.0` for non-zero v
    - Verify 0.0 for nil/empty/mismatched/zero-magnitude inputs
    - Verify result matches `dot(a,b) / (|a| * |b|)` formula
    - **Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5**

  - [x] 1.3 Implement `ToolEmbeddingCache` in `corelib/tool/hybrid.go`
    - Cache keyed by SHA-256 hash of description text
    - `Get(text string) ([]float32, error)` — returns cached or computes new
    - `GetBatch(texts map[string]string) (map[string][]float32, error)` — batch variant
    - Use `sync.RWMutex` for concurrent read safety
    - Skip vector score on per-tool embed error (return nil, no error propagation)
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [ ]* 1.4 Write property test for ToolEmbeddingCache deduplication
    - **Property 4: Tool embedding cache deduplication**
    - Use mock embedder with call counter
    - Generate random string sequences, verify `Embed()` called once per unique text
    - **Validates: Requirements 2.1, 2.2, 2.3**

- [x] 2. Implement QueryEmbeddingCache and HybridRetriever
  - [x] 2.1 Implement `QueryEmbeddingCache` in `corelib/tool/hybrid.go`
    - LRU cache with TTL for user query embeddings
    - `maxSize = 64`, `ttl = 30s` defaults
    - `Get(query string) ([]float32, error)` — returns cached or computes new
    - Evict least recently used entry when at capacity
    - Use `sync.Mutex` for thread safety
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

  - [ ]* 2.2 Write property test for QueryEmbeddingCache deduplication
    - **Property 6: Query embedding cache deduplication**
    - Verify same query within TTL invokes `Embed()` only once
    - **Validates: Requirements 7.2**

  - [ ]* 2.3 Write property test for QueryEmbeddingCache LRU eviction
    - **Property 7: Query cache LRU eviction**
    - Insert N > maxSize distinct queries, verify cache size ≤ maxSize
    - Verify evicted entry triggers new `Embed` call on re-access
    - **Validates: Requirements 7.3, 7.5**

  - [x] 2.4 Implement `HybridRetriever` in `corelib/tool/hybrid.go`
    - `NewHybridRetriever(emb embedding.Embedder) *HybridRetriever`
    - Default `alpha = 0.6`
    - `FuseScores(query, bm25Scores, toolTexts)` method:
      - If embedder is NoopEmbedder, return bm25Scores unchanged
      - Get query embedding via QueryEmbeddingCache
      - Get tool embeddings via ToolEmbeddingCache
      - Min-max normalize BM25 scores
      - Compute `final = alpha * norm_bm25 + (1-alpha) * cosine_sim`
    - On query embed error, fall back to pure BM25 scores
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7_

  - [ ]* 2.5 Write property test for Noop fallback
    - **Property 1: Noop fallback preserves BM25 behavior**
    - Generate random BM25 score maps and tool descriptions
    - Verify FuseScores with NoopEmbedder returns identical BM25 scores
    - **Validates: Requirements 1.2, 3.3, 4.3**

  - [ ]* 2.6 Write property test for min-max normalization bounds
    - **Property 2: Min-max normalization bounds**
    - Generate random score maps with ≥2 distinct values
    - Verify all normalized scores in [0.0, 1.0], min→0.0, max→1.0
    - **Validates: Requirements 1.4**

  - [ ]* 2.7 Write property test for fusion formula correctness
    - **Property 3: Fusion formula correctness**
    - Generate random alpha, normalized BM25 scores, cosine values
    - Verify `fused == alpha * norm_bm25 + (1-alpha) * cosine_sim`
    - **Validates: Requirements 1.3, 1.5**

- [x] 3. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Integrate HybridRetriever into Router
  - [x] 4.1 Add `hybrid *HybridRetriever` field and `SetEmbedder(emb)` method to `Router` in `corelib/tool/router.go`
    - `SetEmbedder` checks for NoopEmbedder: if noop, set hybrid=nil; otherwise create HybridRetriever
    - _Requirements: 3.1, 3.3_

  - [x] 4.2 Modify `Router.Route()` to use `hybrid.FuseScores()` when hybrid is non-nil
    - Build toolTexts map from candidate names and descriptions
    - Pass BM25 scores and toolTexts to `FuseScores()`
    - Use fused scores for sorting instead of raw BM25 scores
    - Embed user query once per Route call (via QueryEmbeddingCache inside HybridRetriever)
    - On embed error, fall back to BM25 (handled inside FuseScores)
    - Add debug-level logging of top-5 tools with BM25/vector/fused scores when hybrid active
    - _Requirements: 3.2, 3.4, 3.5, 8.1, 8.3_

  - [ ]* 4.3 Write property test for single query embed per Route call
    - **Property 8: Single query embed per Route call**
    - Use mock embedder with call counter
    - Generate random user message + random candidate tools
    - Verify query embed called at most once per Route() call
    - **Validates: Requirements 3.4**

- [x] 5. Integrate HybridRetriever into DynamicToolBuilder
  - [x] 5.1 Add `hybrid *HybridRetriever` field and `SetEmbedder(emb)` method to `DynamicToolBuilder` in `corelib/tool/builder.go`
    - Same pattern as Router: check NoopEmbedder, create/nil HybridRetriever
    - _Requirements: 4.1, 4.3_

  - [x] 5.2 Modify `DynamicToolBuilder.Build()` to use `hybrid.FuseScores()` when hybrid is non-nil
    - Build toolTexts map from dynamic tool names and descriptions
    - Pass BM25 scores and toolTexts to `FuseScores()`
    - Use fused scores for sorting instead of raw BM25 scores
    - _Requirements: 4.2_

- [x] 6. Wire GUI layer adapters
  - [x] 6.1 Add `SetEmbedder(emb)` bridge method to `ToolRouter` in `gui/tool_router.go`
    - Delegate to `r.inner.SetEmbedder(emb)`
    - _Requirements: 5.1_

  - [x] 6.2 Add `SetEmbedder(emb)` bridge method to `DynamicToolBuilder` in `gui/tool_builder.go`
    - Delegate to `b.inner.SetEmbedder(emb)`
    - _Requirements: 5.1_

  - [x] 6.3 Modify `SetVectorSearchEnabled()` in `gui/app_embedding.go` to inject embedder into Router and Builder
    - When enabled: call `a.toolRouter.SetEmbedder(emb)` and `a.toolBuilder.SetEmbedder(emb)`
    - When disabled: call `SetEmbedder(embedding.NoopEmbedder{})` on both
    - Reuse the same Embedder instance used for memory store
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

  - [x] 6.4 Add `HybridToolRetrievalActive bool` field to `VectorSearchStatus` in `gui/app_embedding.go`
    - Populate based on whether Router's hybrid field is non-nil
    - _Requirements: 8.2_

- [ ] 7. Unit tests for edge cases and integration
  - [ ]* 7.1 Write unit tests in `corelib/tool/hybrid_test.go`
    - TestCosineSimilarity_NilInputs — nil/empty/mismatched vectors return 0.0
    - TestCosineSimilarity_ZeroMagnitude — zero vector returns 0.0
    - TestHybridRetriever_DefaultAlpha — verify default alpha = 0.6
    - TestQueryCache_TTLExpiry — verify re-computation after TTL expires
    - TestQueryCache_DefaultTTL — verify default TTL = 30s
    - TestRouter_SetEmbedder_Noop — NoopEmbedder does not activate hybrid
    - TestRouter_EmbedError_Fallback — embed error falls back to BM25
    - TestToolCache_EmbedError_Skip — per-tool embed error skips vector score
    - _Requirements: 1.2, 1.6, 2.5, 3.3, 3.5, 6.2, 6.4, 7.4_

- [x] 8. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Property tests use `pgregory.net/rapid` with minimum 100 iterations
- Mock embedder with call counter is used across property and unit tests
- Checkpoints ensure incremental validation
- The same Embedder instance is shared across memory store, Router, and Builder to avoid duplicate model loading
