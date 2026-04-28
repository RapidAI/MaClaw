package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func newDedupTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_memory.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func TestSave_SubstringDedup_ContainedContent(t *testing.T) {
	s := newDedupTestStore(t)

	_ = s.Save(Entry{
		Content:  "The project uses PostgreSQL 16 with pgvector extension for vector search and BM25 indexing",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"postgresql", "pgvector"},
	})

	_ = s.Save(Entry{
		Content:  "The project uses PostgreSQL 16 with pgvector extension",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"database"},
	})

	entries := s.List(CategoryProjectKnowledge, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after substring dedup, got %d", len(entries))
	}

	hasPgvector := false
	hasDatabase := false
	for _, tag := range entries[0].Tags {
		if tag == "pgvector" {
			hasPgvector = true
		}
		if tag == "database" {
			hasDatabase = true
		}
	}
	if !hasPgvector || !hasDatabase {
		t.Errorf("expected merged tags, got: %v", entries[0].Tags)
	}
}

func TestSave_SubstringDedup_ContainingContent(t *testing.T) {
	s := newDedupTestStore(t)

	_ = s.Save(Entry{
		Content:  "User prefers dark mode in all editors and terminals",
		Category: CategoryPreference,
	})

	_ = s.Save(Entry{
		Content:  "User prefers dark mode in all editors and terminals, especially VS Code and iTerm2",
		Category: CategoryPreference,
	})

	entries := s.List(CategoryPreference, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after substring dedup, got %d", len(entries))
	}
}

func TestSave_SubstringDedup_ShortContentNotDeduped(t *testing.T) {
	s := newDedupTestStore(t)

	_ = s.Save(Entry{Content: "use dark mode", Category: CategoryProjectKnowledge})
	_ = s.Save(Entry{Content: "prefer vim", Category: CategoryProjectKnowledge})

	entries := s.List(CategoryProjectKnowledge, "")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for short content, got %d", len(entries))
	}
}

func TestSave_SubstringDedup_DifferentContentNotDeduped(t *testing.T) {
	s := newDedupTestStore(t)

	_ = s.Save(Entry{
		Content:  "The project uses PostgreSQL 16 for the main database backend",
		Category: CategoryProjectKnowledge,
	})
	_ = s.Save(Entry{
		Content:  "The deployment pipeline uses Docker and Kubernetes for orchestration",
		Category: CategoryProjectKnowledge,
	})

	entries := s.List(CategoryProjectKnowledge, "")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for different content, got %d", len(entries))
	}
}

func TestFindSubstringDuplicate_OnlyScansRecent50(t *testing.T) {
	s := newDedupTestStore(t)

	// Save 60 entries with completely distinct content.
	distinctEntries := []string{
		"PostgreSQL 16 supports JSONB indexing for fast document queries",
		"Redis cluster mode requires at least 6 nodes for high availability",
		"Docker compose version 3 uses YAML syntax for service definitions",
		"Kubernetes pods can have multiple containers sharing network namespace",
		"Terraform state files should be stored in remote backends like S3",
		"Nginx reverse proxy configuration uses upstream blocks for load balancing",
		"React hooks replaced class components for state management in functional components",
		"Vue 3 composition API provides better TypeScript support than options API",
		"Angular dependency injection uses hierarchical injector tree pattern",
		"Svelte compiles components to vanilla JavaScript at build time",
		"Git rebase interactive mode allows squashing commits before merge",
		"SSH key authentication is more secure than password-based login",
		"TLS 1.3 removed support for older cipher suites like RC4 and DES",
		"GraphQL subscriptions use WebSocket protocol for real-time data",
		"gRPC uses Protocol Buffers for efficient binary serialization",
		"MongoDB sharding distributes data across multiple replica sets",
		"Elasticsearch inverted index enables full-text search capabilities",
		"RabbitMQ exchanges route messages to queues based on binding keys",
		"Apache Kafka partitions enable parallel consumption of topic messages",
		"Prometheus scrapes metrics endpoints at configurable intervals",
		"Grafana dashboards visualize time-series data from multiple sources",
		"Jenkins pipeline as code defines CI/CD workflows in Jenkinsfile",
		"GitHub Actions workflows trigger on push pull request and schedule events",
		"AWS Lambda functions have 15 minute maximum execution timeout",
		"Azure Functions consumption plan charges per execution and memory usage",
		"GCP Cloud Run automatically scales containers based on request traffic",
		"Rust ownership system prevents data races at compile time",
		"Go goroutines are lightweight threads managed by the Go runtime scheduler",
		"Python asyncio event loop handles concurrent IO operations efficiently",
		"Java virtual threads in Project Loom simplify concurrent programming",
		"TypeScript discriminated unions enable exhaustive pattern matching",
		"C++ smart pointers automatically manage heap memory deallocation",
		"Haskell monads compose side effects in pure functional programs",
		"Elixir GenServer processes handle synchronous and asynchronous messages",
		"Scala implicits provide type class instances for ad-hoc polymorphism",
		"Kotlin coroutines use structured concurrency for lifecycle management",
		"Swift actors isolate mutable state for safe concurrent access",
		"Zig comptime evaluates expressions at compile time for zero-cost abstractions",
		"OCaml algebraic data types enable exhaustive pattern matching",
		"Clojure persistent data structures use structural sharing for efficiency",
		"WebAssembly modules run in sandboxed linear memory environment",
		"LLVM intermediate representation enables cross-platform code generation",
		"V8 engine uses hidden classes for optimized JavaScript property access",
		"SQLite WAL mode allows concurrent readers with single writer",
		"CockroachDB uses Raft consensus for distributed transaction commits",
		"Cassandra uses consistent hashing for partition key distribution",
		"Neo4j Cypher query language traverses graph relationships efficiently",
		"InfluxDB retention policies automatically expire old time-series data",
		"MinIO provides S3-compatible object storage for on-premises deployment",
		"Vault dynamic secrets rotate database credentials automatically",
		"Consul service mesh provides mutual TLS between microservices",
		"Istio sidecar proxy intercepts all pod network traffic transparently",
		"Envoy proxy supports HTTP/2 gRPC and WebSocket protocols natively",
		"HAProxy health checks remove unhealthy backends from load balancer pool",
		"Traefik auto-discovers services from Docker labels and Kubernetes ingress",
		"Caddy web server automatically obtains and renews TLS certificates",
		"OpenTelemetry collector receives traces metrics and logs from applications",
		"Jaeger distributed tracing visualizes request flow across microservices",
		"Fluentd unified logging layer routes logs to multiple destinations",
		"Loki log aggregation indexes labels not full text for cost efficiency",
	}

	for i := 0; i < 60; i++ {
		_ = s.Save(Entry{
			Content:  distinctEntries[i],
			Category: CategoryProjectKnowledge,
		})
	}

	s.mu.RLock()
	entryCount := len(s.entries)
	s.mu.RUnlock()

	if entryCount < 55 {
		t.Fatalf("expected at least 55 distinct entries, got %d", entryCount)
	}

	s.mu.Lock()
	idx := s.findSubstringDuplicate(s.entries[0].Content, "")
	s.mu.Unlock()

	if idx == 0 {
		t.Error("findSubstringDuplicate should not scan entries outside the recent-50 window")
	}
}

// ---------------------------------------------------------------------------
// Semantic dedup tests (embedding candidate recall + LLM judgment)
// ---------------------------------------------------------------------------

// fakeEmbedderForDedup returns pre-configured embeddings for test content.
type fakeEmbedderForDedup struct {
	vectors map[string][]float32
	dim     int
}

func (f *fakeEmbedderForDedup) Embed(text string) ([]float32, error) {
	if v, ok := f.vectors[text]; ok {
		return v, nil
	}
	// Return a zero vector for unknown text (won't match anything).
	return make([]float32, f.dim), nil
}

func (f *fakeEmbedderForDedup) EmbedBatch(texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, t := range texts {
		v, _ := f.Embed(t)
		result[i] = v
	}
	return result, nil
}

func (f *fakeEmbedderForDedup) Dim() int { return f.dim }
func (f *fakeEmbedderForDedup) Close()   {}

// fakeLLMDedup simulates LLM dedup judgment.
type fakeLLMDedup struct {
	// mergeAll: if true, always returns "merge"; if false, always returns "keep"
	mergeAll bool
}

func (f *fakeLLMDedup) ChatCall(messages []map[string]string) (string, error) {
	if f.mergeAll {
		return `{"decision": "merge", "merged": "merged content from both entries", "reason": "same fact"}`, nil
	}
	return `{"decision": "keep", "reason": "different facts"}`, nil
}

func (f *fakeLLMDedup) IsConfigured() bool { return true }

func TestSemanticDedup_CandidateRecall_HighSimilarity(t *testing.T) {
	s := newDedupTestStore(t)

	// Two entries with very high cosine similarity (0.95).
	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.95, 0.05, 0, 0} // cosine ≈ 0.999 with A (both L2-normalized)

	emb := &fakeEmbedderForDedup{
		dim: 4,
		vectors: map[string][]float32{
			"maclaw 使用 Go 语言开发，前端使用 Wails 框架":  vecA,
			"maclaw 项目基于 Go 语言和 Wails 框架开发":     vecB,
		},
	}
	s.SetEmbedder(emb)

	_ = s.Save(Entry{
		Content:   "maclaw 使用 Go 语言开发，前端使用 Wails 框架",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	})

	_ = s.Save(Entry{
		Content:  "maclaw 项目基于 Go 语言和 Wails 框架开发",
		Category: CategoryProjectKnowledge,
		// Embedding will be computed by SaveWithContext via embedder
	})

	// Both entries should be saved (embedding dedup doesn't merge immediately).
	s.mu.RLock()
	count := len(s.entries)
	pendingCount := len(s.pendingDedup)
	s.mu.RUnlock()

	if count != 2 {
		t.Fatalf("expected 2 entries (dedup is async), got %d", count)
	}
	if pendingCount != 1 {
		t.Fatalf("expected 1 pending dedup pair, got %d", pendingCount)
	}
}

func TestSemanticDedup_CandidateRecall_LowSimilarity(t *testing.T) {
	s := newDedupTestStore(t)

	// Two entries with low cosine similarity (orthogonal vectors).
	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0, 1, 0, 0} // cosine = 0 with A

	emb := &fakeEmbedderForDedup{
		dim: 4,
		vectors: map[string][]float32{
			"PostgreSQL 数据库性能优化指南": vecA,
			"Redis 缓存集群部署方案":      vecB,
		},
	}
	s.SetEmbedder(emb)

	_ = s.Save(Entry{
		Content:   "PostgreSQL 数据库性能优化指南",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	})
	_ = s.Save(Entry{
		Content:  "Redis 缓存集群部署方案",
		Category: CategoryProjectKnowledge,
	})

	s.mu.RLock()
	count := len(s.entries)
	pendingCount := len(s.pendingDedup)
	s.mu.RUnlock()

	if count != 2 {
		t.Fatalf("expected 2 entries, got %d", count)
	}
	if pendingCount != 0 {
		t.Fatalf("expected 0 pending dedup pairs for different topics, got %d", pendingCount)
	}
}

func TestSemanticDedup_ProcessPending_Merge(t *testing.T) {
	s := newDedupTestStore(t)

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.95, 0.05, 0, 0}

	emb := &fakeEmbedderForDedup{
		dim: 4,
		vectors: map[string][]float32{
			"用户偏好使用深色主题和等宽字体进行日常编程工作": vecA,
			"用户喜欢深色主题配合等宽字体来编程和写代码":   vecB,
		},
	}
	s.SetEmbedder(emb)
	s.SetLLMDedup(&fakeLLMDedup{mergeAll: true})

	_ = s.Save(Entry{
		Content:   "用户偏好使用深色主题和等宽字体进行日常编程工作",
		Category:  CategoryPreference,
		Embedding: vecA,
	})
	_ = s.Save(Entry{
		Content:  "用户喜欢深色主题配合等宽字体来编程和写代码",
		Category: CategoryPreference,
	})

	// Process pending dedup.
	merged := s.ProcessPendingDedup(context.Background())
	if merged != 1 {
		t.Fatalf("expected 1 merge, got %d", merged)
	}

	s.mu.RLock()
	count := len(s.entries)
	pendingCount := len(s.pendingDedup)
	s.mu.RUnlock()

	if count != 1 {
		t.Fatalf("expected 1 entry after merge, got %d", count)
	}
	if pendingCount != 0 {
		t.Fatalf("expected 0 pending after processing, got %d", pendingCount)
	}
}

func TestSemanticDedup_ProcessPending_Keep(t *testing.T) {
	s := newDedupTestStore(t)

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.95, 0.05, 0, 0}

	emb := &fakeEmbedderForDedup{
		dim: 4,
		vectors: map[string][]float32{
			"PostgreSQL 数据库性能优化和索引调优指南":  vecA,
			"PostgreSQL 数据库备份恢复和灾难恢复方案": vecB,
		},
	}
	s.SetEmbedder(emb)
	s.SetLLMDedup(&fakeLLMDedup{mergeAll: false}) // LLM says "keep"

	_ = s.Save(Entry{
		Content:   "PostgreSQL 数据库性能优化和索引调优指南",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	})
	_ = s.Save(Entry{
		Content:  "PostgreSQL 数据库备份恢复和灾难恢复方案",
		Category: CategoryProjectKnowledge,
	})

	merged := s.ProcessPendingDedup(context.Background())
	if merged != 0 {
		t.Fatalf("expected 0 merges (LLM said keep), got %d", merged)
	}

	s.mu.RLock()
	count := len(s.entries)
	s.mu.RUnlock()

	if count != 2 {
		t.Fatalf("expected 2 entries after keep decision, got %d", count)
	}
}

func TestSemanticDedup_CrossCategory_NoCandidateRecall(t *testing.T) {
	s := newDedupTestStore(t)

	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0.95, 0.05, 0, 0}

	emb := &fakeEmbedderForDedup{
		dim: 4,
		vectors: map[string][]float32{
			"maclaw 使用 Go 语言开发，前端使用 Wails 框架": vecA,
			"maclaw 项目基于 Go 语言和 Wails 框架开发":    vecB,
		},
	}
	s.SetEmbedder(emb)

	_ = s.Save(Entry{
		Content:   "maclaw 使用 Go 语言开发，前端使用 Wails 框架",
		Category:  CategoryProjectKnowledge,
		Embedding: vecA,
	})
	_ = s.Save(Entry{
		Content:  "maclaw 项目基于 Go 语言和 Wails 框架开发",
		Category: CategoryInstruction, // different canonical category
	})

	s.mu.RLock()
	pendingCount := len(s.pendingDedup)
	s.mu.RUnlock()

	if pendingCount != 0 {
		t.Fatalf("expected 0 pending dedup for cross-category, got %d", pendingCount)
	}
}

func TestSemanticDedup_NoEmbedder_Noop(t *testing.T) {
	s := newDedupTestStore(t)
	// No embedder set — semantic dedup should be a no-op.

	_ = s.Save(Entry{
		Content:  "maclaw 使用 Go 语言开发，前端使用 Wails 框架",
		Category: CategoryProjectKnowledge,
	})
	_ = s.Save(Entry{
		Content:  "maclaw 项目基于 Go 语言和 Wails 框架开发",
		Category: CategoryProjectKnowledge,
	})

	s.mu.RLock()
	count := len(s.entries)
	pendingCount := len(s.pendingDedup)
	s.mu.RUnlock()

	if count != 2 {
		t.Fatalf("expected 2 entries without embedder, got %d", count)
	}
	if pendingCount != 0 {
		t.Fatalf("expected 0 pending without embedder, got %d", pendingCount)
	}
}
