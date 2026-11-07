package memory

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type extractionQueueTestLLM struct {
	mu              sync.Mutex
	extractResponse string
	extractErr      error
	extractCalls    int
	started         chan struct{}
	block           chan struct{}
	startOnce       sync.Once
}

func (m *extractionQueueTestLLM) IsConfigured() bool { return true }

func (m *extractionQueueTestLLM) ChatCall(messages []map[string]string) (string, error) {
	extraction := false
	for _, message := range messages {
		if message["role"] == "system" && contains(message["content"], "memory extraction assistant") {
			extraction = true
			break
		}
	}
	if !extraction {
		return `{"operation":"noop","target_id":"","merged_text":"","reason":"test"}`, nil
	}
	m.mu.Lock()
	m.extractCalls++
	response, err := m.extractResponse, m.extractErr
	started, block := m.started, m.block
	m.mu.Unlock()
	if started != nil {
		m.startOnce.Do(func() { close(started) })
	}
	if block != nil {
		<-block
	}
	return response, err
}

func (m *extractionQueueTestLLM) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.extractCalls
}

func waitForExtractionQueue(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for online extraction queue")
}

func TestExtractionQueueCoalescesCooldownWithoutDroppingMessages(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	llm := &extractionQueueTestLLM{extractResponse: `[]`}
	extractor := NewOnlineExtractor(store, llm)
	extractor.SetCooldown(50 * time.Millisecond)
	defer extractor.Stop()
	var paused atomic.Bool
	paused.Store(true)
	extractor.SetQueuePauseChecker(func(string) bool { return paused.Load() })
	base := []ConversationMessage{{ID: "u1", Role: "user", Content: "first"}, {ID: "a1", Role: "assistant", Content: "ok"}}
	if err := extractor.EnqueueExtraction(context.Background(), base, "", time.Now(), "owner"); err != nil {
		t.Fatal(err)
	}
	next := append(append([]ConversationMessage(nil), base...), ConversationMessage{ID: "u2", Role: "user", Content: "second"}, ConversationMessage{ID: "a2", Role: "assistant", Content: "done"})
	if err := extractor.EnqueueExtraction(context.Background(), next, "", time.Now(), "owner"); err != nil {
		t.Fatal(err)
	}
	queue := extractor.extractionQueue()
	if got := queue.pendingCount(); got != 1 {
		t.Fatalf("pending requests = %d, want one coalesced request", got)
	}
	queue.mu.Lock()
	messages := len(queue.pending[onlineExtractionRequestKey("owner", "")].Messages)
	queue.mu.Unlock()
	if messages != 4 {
		t.Fatalf("coalesced messages = %d, want 4", messages)
	}
	paused.Store(false)
	queue.start()
	waitForExtractionQueue(t, func() bool { return queue.pendingCount() == 0 })
	if got := llm.calls(); got != 1 {
		t.Fatalf("extraction calls = %d, want one coalesced call", got)
	}
}

func TestExtractionQueueRetriesAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memories.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	failing := &extractionQueueTestLLM{extractErr: errors.New("temporary extraction failure")}
	first := NewOnlineExtractor(store, failing)
	messages := []ConversationMessage{
		{ID: "u1", Role: "user", Content: "User prefers dark mode for all IDEs"},
		{ID: "a1", Role: "assistant", Content: "Noted"},
	}
	if err := first.EnqueueExtraction(context.Background(), messages, "", time.Now(), "owner"); err != nil {
		t.Fatal(err)
	}
	queue := first.extractionQueue()
	waitForExtractionQueue(t, func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()
		request := queue.pending[onlineExtractionRequestKey("owner", "")]
		return request != nil && request.Attempts > 0
	})
	first.Stop()

	restored := NewOnlineExtractor(store, nil)
	if got := restored.extractionQueue().pendingCount(); got != 1 {
		t.Fatalf("restored pending requests = %d, want 1", got)
	}
	succeeding := &extractionQueueTestLLM{extractResponse: `[{"content":"User prefers dark mode for all IDEs and editors","category":"preference"}]`}
	restored.SetLLM(succeeding)
	waitForExtractionQueue(t, func() bool { return restored.extractionQueue().pendingCount() == 0 })
	if got := store.List(CategoryPreference, "dark mode"); len(got) != 1 {
		t.Fatalf("restored extraction did not write memory: %+v", got)
	}
	restored.Stop()
}

func TestExtractionQueueKeepsMessagesAddedDuringProcessing(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	started := make(chan struct{})
	block := make(chan struct{})
	llm := &extractionQueueTestLLM{extractResponse: `[]`, started: started, block: block}
	extractor := NewOnlineExtractor(store, llm)
	extractor.SetCooldown(0)
	defer extractor.Stop()
	messages := []ConversationMessage{{ID: "u1", Role: "user", Content: "first"}, {ID: "a1", Role: "assistant", Content: "ok"}}
	if err := extractor.EnqueueExtraction(context.Background(), messages, "", time.Now(), "owner"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("extraction did not start")
	}
	if err := extractor.EnqueueExtraction(context.Background(), []ConversationMessage{{ID: "u2", Role: "user", Content: "second"}, {ID: "a2", Role: "assistant", Content: "done"}}, "", time.Now(), "owner"); err != nil {
		t.Fatal(err)
	}
	var paused atomic.Bool
	extractor.SetQueuePauseChecker(func(string) bool { return paused.Load() })
	paused.Store(true)
	close(block)
	queue := extractor.extractionQueue()
	waitForExtractionQueue(t, func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()
		request := queue.pending[onlineExtractionRequestKey("owner", "")]
		return request != nil && len(request.Messages) == 2 && request.Messages[0].ID == "u2"
	})
	paused.Store(false)
	queue.start()
	waitForExtractionQueue(t, func() bool { return queue.pendingCount() == 0 })
	if got := llm.calls(); got != 2 {
		t.Fatalf("extraction calls = %d, want original and newly appended batches", got)
	}
}

func TestExtractionQueueSeparatesProjectsForSameOwner(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	llm := &extractionQueueTestLLM{extractResponse: `[]`}
	extractor := NewOnlineExtractor(store, llm)
	defer extractor.Stop()
	var paused atomic.Bool
	paused.Store(true)
	extractor.SetQueuePauseChecker(func(string) bool { return paused.Load() })
	messages := []ConversationMessage{{ID: "u1", Role: "user", Content: "project fact"}}
	for _, projectPath := range []string{`D:\work\a`, `D:\work\b`} {
		if err := extractor.EnqueueExtraction(context.Background(), messages, "", time.Now(), "owner", WriteContext{OwnerID: "owner", ProjectPath: projectPath}); err != nil {
			t.Fatal(err)
		}
	}
	if got := extractor.extractionQueue().pendingCount(); got != 2 {
		t.Fatalf("pending project queues = %d, want 2", got)
	}
}

func TestExtractionQueueCancellationRetainsUncommittedRequest(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	started := make(chan struct{})
	block := make(chan struct{})
	llm := &extractionQueueTestLLM{extractResponse: `[]`, started: started, block: block}
	extractor := NewOnlineExtractor(store, llm)
	messages := []ConversationMessage{{ID: "u1", Role: "user", Content: "first"}, {ID: "a1", Role: "assistant", Content: "ok"}}
	if err := extractor.EnqueueExtraction(context.Background(), messages, "", time.Now(), "owner"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("extraction did not start")
	}
	stopped := make(chan struct{})
	go func() {
		extractor.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("queue stop did not cancel the active attempt")
	}
	close(block)
	queue := extractor.extractionQueue()
	if got := queue.pendingCount(); got != 1 {
		t.Fatalf("cancelled request was dropped: pending=%d", got)
	}
	queue.mu.Lock()
	attempts := queue.pending[onlineExtractionRequestKey("owner", "")].Attempts
	queue.mu.Unlock()
	if attempts != 1 {
		t.Fatalf("cancelled request attempts = %d, want 1", attempts)
	}
}

func TestMaintenanceInstallRuntimeRestoresPendingExtraction(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	failing := &extractionQueueTestLLM{extractErr: errors.New("persist pending")}
	first := NewOnlineExtractor(store, failing)
	messages := []ConversationMessage{{ID: "u1", Role: "user", Content: "User prefers dark mode for all IDEs"}, {ID: "a1", Role: "assistant", Content: "Noted"}}
	if err := first.EnqueueExtraction(context.Background(), messages, "", time.Now(), "owner"); err != nil {
		t.Fatal(err)
	}
	queue := first.extractionQueue()
	waitForExtractionQueue(t, func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()
		request := queue.pending[onlineExtractionRequestKey("owner", "")]
		return request != nil && request.Attempts > 0
	})
	first.Stop()

	succeeding := &extractionQueueTestLLM{extractResponse: `[{"content":"User prefers dark mode for all IDEs and editors","category":"preference"}]`}
	maintenance := NewMaintenance(store, succeeding, nil)
	maintenance.InstallRuntime()
	defer maintenance.Stop()
	waitForExtractionQueue(t, func() bool {
		return len(store.List(CategoryPreference, "dark mode")) == 1
	})
}
