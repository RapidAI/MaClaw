package memory

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type pipelineTestEmitter struct {
	count int32
	ch    chan struct{}
}

func (e *pipelineTestEmitter) Emit(eventType string, payload interface{}) {
	if eventType != "memory:pipeline_completed" {
		return
	}
	atomic.AddInt32(&e.count, 1)
	select {
	case e.ch <- struct{}{}:
	default:
	}
}

func (e *pipelineTestEmitter) Subscribe(eventType string, handler corelib.EventHandler) {}

func TestPipelineTriggerSoonDebouncesBursts(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()
	emitter := &pipelineTestEmitter{ch: make(chan struct{}, 4)}
	pipeline := NewPipeline(store, nil, nil, nil, emitter)

	pipeline.TriggerSoon(20 * time.Millisecond)
	pipeline.TriggerSoon(20 * time.Millisecond)
	pipeline.TriggerSoon(20 * time.Millisecond)

	select {
	case <-emitter.ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for debounced pipeline run")
	}
	select {
	case <-emitter.ch:
		t.Fatal("expected burst to debounce to one run")
	case <-time.After(80 * time.Millisecond):
	}
	if got := atomic.LoadInt32(&emitter.count); got != 1 {
		t.Fatalf("pipeline run count = %d, want 1", got)
	}
}

func TestPipelineStartDelayedDefersInitialRun(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()
	emitter := &pipelineTestEmitter{ch: make(chan struct{}, 4)}
	pipeline := NewPipeline(store, nil, nil, nil, emitter)
	pipeline.StartDelayed(80 * time.Millisecond)
	defer pipeline.Stop()

	select {
	case <-emitter.ch:
		t.Fatal("pipeline ran before initial delay")
	case <-time.After(30 * time.Millisecond):
	}

	select {
	case <-emitter.ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for delayed pipeline run")
	}
}
