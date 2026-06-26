package memory

import (
	"sync"
	"testing"
	"time"
)

type countingQueryEmbedder struct {
	calls int
	dim   int
}

func (e *countingQueryEmbedder) Embed(text string) ([]float32, error) {
	e.calls++
	return []float32{float32(len(text)), 1, 0, 0}, nil
}

func (e *countingQueryEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, err
		}
		out[i] = vec
	}
	return out, nil
}

func (e *countingQueryEmbedder) Dim() int { return e.dim }
func (e *countingQueryEmbedder) Close()   {}

func TestQueryEmbeddingCachedReusesEmbedding(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	emb := &countingQueryEmbedder{dim: 4}
	store.embedder = emb
	query := "tell me two jokes"

	first := store.queryEmbeddingCached(query)
	second := store.queryEmbeddingCached(query)
	if emb.calls != 1 {
		t.Fatalf("embed calls = %d, want 1", emb.calls)
	}
	if len(first) != len(second) || len(first) == 0 {
		t.Fatalf("unexpected cached vectors: first=%v second=%v", first, second)
	}
	second[0] = 99
	third := store.queryEmbeddingCached(query)
	if third[0] == 99 {
		t.Fatal("cached query embedding leaked mutable slice")
	}
}

func TestSetEmbedderClearsQueryEmbeddingCache(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	first := &countingQueryEmbedder{dim: 4}
	store.SetEmbedder(first)
	if got := store.queryEmbeddingCached("same query"); len(got) == 0 {
		t.Fatal("first query embedding is empty")
	}

	second := &countingQueryEmbedder{dim: 4}
	store.SetEmbedder(second)
	if got := store.queryEmbeddingCached("same query"); len(got) == 0 {
		t.Fatal("second query embedding is empty")
	}
	if second.calls != 1 {
		t.Fatalf("second embedder calls = %d, want 1 after cache invalidation", second.calls)
	}
}

type blockingQueryEmbedder struct {
	started chan struct{}
	release chan struct{}
	vec     []float32
}

func (e *blockingQueryEmbedder) Embed(text string) ([]float32, error) {
	close(e.started)
	<-e.release
	return append([]float32(nil), e.vec...), nil
}

func (e *blockingQueryEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, err
		}
		out[i] = vec
	}
	return out, nil
}

func (e *blockingQueryEmbedder) Dim() int { return len(e.vec) }
func (e *blockingQueryEmbedder) Close()   {}

func TestSetEmbedderInvalidatesInFlightQueryEmbedding(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	first := &blockingQueryEmbedder{started: make(chan struct{}), release: make(chan struct{}), vec: []float32{1, 0, 0, 0}}
	store.SetEmbedder(first)
	done := make(chan []float32, 1)
	go func() {
		done <- store.queryEmbeddingCached("same query")
	}()
	<-first.started

	second := &countingQueryEmbedder{dim: 4}
	store.SetEmbedder(second)
	close(first.release)
	<-done

	if got := store.queryEmbeddingCached("same query"); len(got) == 0 || got[0] == 1 {
		t.Fatalf("query cache reused stale in-flight embedding: %v", got)
	}
	if second.calls != 1 {
		t.Fatalf("second embedder calls = %d, want 1", second.calls)
	}
}

type synchronizedQueryEmbedder struct {
	mu    sync.Mutex
	calls int
	dim   int
	wait  chan struct{}
}

func (e *synchronizedQueryEmbedder) Embed(text string) ([]float32, error) {
	if e.wait != nil {
		<-e.wait
	}
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	return []float32{float32(len(text)), 1, 0, 0}, nil
}

func (e *synchronizedQueryEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, err
		}
		out[i] = vec
	}
	return out, nil
}

func (e *synchronizedQueryEmbedder) Dim() int { return e.dim }
func (e *synchronizedQueryEmbedder) Close()   {}
func (e *synchronizedQueryEmbedder) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestQueryEmbeddingCachedCoalescesConcurrentMisses(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	release := make(chan struct{})
	emb := &synchronizedQueryEmbedder{dim: 4, wait: release}
	store.SetEmbedder(emb)

	const callers = 8
	var wg sync.WaitGroup
	wg.Add(callers)
	results := make(chan []float32, callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			results <- store.queryEmbeddingCached("same concurrent query")
		}()
	}
	close(release)
	wg.Wait()
	close(results)

	for got := range results {
		if len(got) != 4 {
			t.Fatalf("query embedding len = %d, want 4", len(got))
		}
	}
	if emb.Calls() != 1 {
		t.Fatalf("embed calls = %d, want 1", emb.Calls())
	}
}

func TestSetEmbedderDoesNotStrandInFlightQueryWaiter(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	first := &blockingQueryEmbedder{started: make(chan struct{}), release: make(chan struct{}), vec: []float32{1, 0, 0, 0}}
	store.SetEmbedder(first)

	firstDone := make(chan []float32, 1)
	secondDone := make(chan []float32, 1)
	go func() { firstDone <- store.queryEmbeddingCached("same query") }()
	<-first.started
	go func() { secondDone <- store.queryEmbeddingCached("same query") }()

	store.SetEmbedder(&countingQueryEmbedder{dim: 4})
	close(first.release)

	select {
	case got := <-firstDone:
		if got != nil {
			t.Fatalf("first in-flight query returned stale embedding: %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first in-flight query waiter was stranded")
	}
	select {
	case got := <-secondDone:
		if len(got) > 0 && got[0] == 1 {
			t.Fatalf("second in-flight query returned stale embedding: %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second in-flight query waiter was stranded")
	}
}

func TestQueryEmbeddingCachedTimesOutButWarmsCacheLater(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	prevBudget := queryEmbeddingWaitBudget
	queryEmbeddingWaitBudget = 30 * time.Millisecond
	t.Cleanup(func() { queryEmbeddingWaitBudget = prevBudget })

	emb := &blockingQueryEmbedder{
		started: make(chan struct{}),
		release: make(chan struct{}),
		vec:     []float32{7, 0, 0, 0},
	}
	store.SetEmbedder(emb)

	start := time.Now()
	got := store.queryEmbeddingCached("slow query")
	if got != nil {
		t.Fatalf("expected timeout fallback, got %v", got)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("timeout fallback took too long: %v", elapsed)
	}

	<-emb.started
	close(emb.release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got = store.queryEmbeddingCached("slow query")
		if len(got) == 4 && got[0] == 7 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("query embedding cache was not warmed after background completion")
}

func TestQueryEmbeddingCachedWaiterTimesOutOnExistingFlight(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	prevBudget := queryEmbeddingWaitBudget
	queryEmbeddingWaitBudget = 30 * time.Millisecond
	t.Cleanup(func() { queryEmbeddingWaitBudget = prevBudget })

	emb := &blockingQueryEmbedder{
		started: make(chan struct{}),
		release: make(chan struct{}),
		vec:     []float32{3, 0, 0, 0},
	}
	store.SetEmbedder(emb)

	firstDone := make(chan []float32, 1)
	go func() {
		firstDone <- store.queryEmbeddingCached("shared slow query")
	}()
	<-emb.started

	start := time.Now()
	second := store.queryEmbeddingCached("shared slow query")
	if second != nil {
		t.Fatalf("expected second waiter to time out, got %v", second)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("second waiter took too long: %v", elapsed)
	}

	close(emb.release)
	<-firstDone
}
