package agent

import (
	"context"
	"sync"
	"testing"
	"time"
)

type mockConnector struct {
	name       string
	configured bool
	items      []DataItem
	fetchErr   error
}

func (m *mockConnector) Name() string          { return m.name }
func (m *mockConnector) IsConfigured() bool    { return m.configured }
func (m *mockConnector) FetchNew(ctx context.Context, since time.Time) ([]DataItem, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.items, nil
}

func TestAutoFetchEngine_FetchOnce(t *testing.T) {
	var mu sync.Mutex
	var received []DataItem

	sink := func(items []DataItem) error {
		mu.Lock()
		received = append(received, items...)
		mu.Unlock()
		return nil
	}

	engine := NewAutoFetchEngine(sink, time.Minute)
	engine.AddConnector(&mockConnector{
		name:       "test",
		configured: true,
		items: []DataItem{
			{Source: "test", Title: "Item 1", Content: "content 1"},
			{Source: "test", Title: "Item 2", Content: "content 2"},
		},
	})

	count, err := engine.FetchOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 items, got %d", count)
	}
	mu.Lock()
	if len(received) != 2 {
		t.Errorf("sink received %d items", len(received))
	}
	mu.Unlock()
}

func TestAutoFetchEngine_SkipsUnconfigured(t *testing.T) {
	callCount := 0
	sink := func(items []DataItem) error {
		callCount++
		return nil
	}

	engine := NewAutoFetchEngine(sink, time.Minute)
	engine.AddConnector(&mockConnector{
		name:       "unconfigured",
		configured: false,
		items:      []DataItem{{Title: "should not appear"}},
	})

	engine.FetchOnce(context.Background())
	if callCount != 0 {
		t.Error("unconfigured connector should be skipped")
	}
}

func TestAutoFetchEngine_MultipleConnectors(t *testing.T) {
	var received []DataItem
	sink := func(items []DataItem) error {
		received = append(received, items...)
		return nil
	}

	engine := NewAutoFetchEngine(sink, time.Minute)
	engine.AddConnector(&mockConnector{
		name: "rss", configured: true,
		items: []DataItem{{Source: "rss", Title: "RSS item"}},
	})
	engine.AddConnector(&mockConnector{
		name: "github", configured: true,
		items: []DataItem{{Source: "github", Title: "GH release"}},
	})

	count, _ := engine.FetchOnce(context.Background())
	if count != 2 {
		t.Errorf("expected 2 total items, got %d", count)
	}
	if len(received) != 2 {
		t.Errorf("sink received %d items", len(received))
	}
}

func TestAutoFetchEngine_NilSafe(t *testing.T) {
	var engine *AutoFetchEngine
	engine.AddConnector(&mockConnector{name: "x"})
	engine.Start()
	engine.Stop()
	count, err := engine.FetchOnce(context.Background())
	if count != 0 || err != nil {
		t.Error("nil engine should be safe")
	}
}

func TestAutoFetchEngine_StartStop(t *testing.T) {
	engine := NewAutoFetchEngine(nil, 50*time.Millisecond)
	engine.Start()
	if !engine.IsRunning() {
		t.Error("should be running")
	}
	time.Sleep(100 * time.Millisecond)
	engine.Stop()
	if engine.IsRunning() {
		t.Error("should be stopped")
	}
}
