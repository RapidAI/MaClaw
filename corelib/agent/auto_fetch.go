package agent

// AutoFetch: periodic data connector that pulls fresh context from external sources.
// Inspired by OpenHuman's auto-fetch (20-minute sync loop) — the agent has
// tomorrow's context this morning, without the user needing to ask.
//
// Connectors are pluggable. Initial built-in connectors:
// - RSS feeds
// - File watch (monitor directories for changes)
// - GitHub (new releases, issue updates)
//
// Each connector implements DataConnector interface. The engine runs all
// connectors on a configurable interval and feeds results into memory.

import (
	"context"
	"log"
	"sync"
	"time"
)

// DataItem represents a piece of data fetched from an external source.
type DataItem struct {
	Source    string    // connector name (e.g. "github", "rss", "file_watch")
	Title     string    // short title
	Content   string    // main content (will be compressed by TokenJuice before storage)
	URL       string    // source URL (for attribution)
	Timestamp time.Time // when this item was created/published
	Tags      []string  // searchable tags
}

// DataConnector is the interface for external data sources.
type DataConnector interface {
	// Name returns the connector identifier.
	Name() string
	// IsConfigured returns true if the connector has valid configuration.
	IsConfigured() bool
	// FetchNew retrieves items created/updated since the given time.
	FetchNew(ctx context.Context, since time.Time) ([]DataItem, error)
}

// AutoFetchSink receives fetched data items for storage.
type AutoFetchSink func(items []DataItem) error

// AutoFetchEngine manages periodic data fetching from external sources.
type AutoFetchEngine struct {
	mu         sync.Mutex
	connectors []DataConnector
	sink       AutoFetchSink
	interval   time.Duration
	stopCh     chan struct{}
	running    bool
	lastFetch  map[string]time.Time // connector name → last successful fetch time
}

// AutoFetchConfig configures the engine.
type AutoFetchConfig struct {
	Enabled  bool          `json:"enabled"`
	Interval time.Duration `json:"interval"` // default 20 minutes
}

// DefaultAutoFetchConfig returns sensible defaults.
func DefaultAutoFetchConfig() AutoFetchConfig {
	return AutoFetchConfig{
		Enabled:  false, // opt-in
		Interval: 20 * time.Minute,
	}
}

// NewAutoFetchEngine creates an engine with the given sink and interval.
func NewAutoFetchEngine(sink AutoFetchSink, interval time.Duration) *AutoFetchEngine {
	if interval <= 0 {
		interval = 20 * time.Minute
	}
	return &AutoFetchEngine{
		sink:      sink,
		interval:  interval,
		stopCh:    make(chan struct{}),
		lastFetch: make(map[string]time.Time),
	}
}

// AddConnector registers a data connector.
func (e *AutoFetchEngine) AddConnector(c DataConnector) {
	if e == nil || c == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.connectors = append(e.connectors, c)
}

// Start begins the periodic fetch loop.
func (e *AutoFetchEngine) Start() {
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	go e.loop()
	log.Printf("[auto-fetch] started with interval=%s, connectors=%d", e.interval, len(e.connectors))
}

// Stop terminates the fetch loop.
func (e *AutoFetchEngine) Stop() {
	if e == nil {
		return
	}
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	e.mu.Unlock()
	close(e.stopCh)
}

// IsRunning returns whether the engine is active.
func (e *AutoFetchEngine) IsRunning() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// FetchOnce runs all connectors once (for testing or manual trigger).
func (e *AutoFetchEngine) FetchOnce(ctx context.Context) (int, error) {
	if e == nil {
		return 0, nil
	}
	return e.fetchAll(ctx)
}

func (e *AutoFetchEngine) loop() {
	// Run immediately on start
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	e.fetchAll(ctx)
	cancel()

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			e.fetchAll(ctx)
			cancel()
		}
	}
}

func (e *AutoFetchEngine) fetchAll(ctx context.Context) (int, error) {
	e.mu.Lock()
	connectors := make([]DataConnector, len(e.connectors))
	copy(connectors, e.connectors)
	e.mu.Unlock()

	totalItems := 0
	for _, c := range connectors {
		if !c.IsConfigured() {
			continue
		}

		since := e.getLastFetch(c.Name())
		items, err := c.FetchNew(ctx, since)
		if err != nil {
			log.Printf("[auto-fetch] %s fetch failed: %v", c.Name(), err)
			continue
		}

		if len(items) > 0 && e.sink != nil {
			if err := e.sink(items); err != nil {
				log.Printf("[auto-fetch] %s sink failed: %v", c.Name(), err)
				continue
			}
			totalItems += len(items)
			log.Printf("[auto-fetch] %s: fetched %d items", c.Name(), len(items))
		}

		e.setLastFetch(c.Name(), time.Now())
	}
	return totalItems, nil
}

func (e *AutoFetchEngine) getLastFetch(name string) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t, ok := e.lastFetch[name]; ok {
		return t
	}
	// Default: fetch last 24 hours on first run
	return time.Now().Add(-24 * time.Hour)
}

func (e *AutoFetchEngine) setLastFetch(name string, t time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastFetch[name] = t
}
