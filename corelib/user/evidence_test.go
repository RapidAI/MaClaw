package user

import (
	"fmt"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	if c.model != m {
		t.Error("collector model not set correctly")
	}
	if len(c.batchQueue) != 0 {
		t.Error("expected empty batch queue")
	}
	if len(c.updateCounts) != 0 {
		t.Error("expected empty update counts")
	}
}

func TestAnalyze_ProgrammingLanguage(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("I've been writing a lot of Python lately")

	p := m.GetProfile()
	if p.PreferredLanguages.Value != "Python" {
		t.Errorf("expected 'Python', got %q", p.PreferredLanguages.Value)
	}
	if p.PreferredLanguages.Confidence != 0.5 {
		t.Errorf("expected confidence 0.5, got %f", p.PreferredLanguages.Confidence)
	}
}

func TestAnalyze_GoLanguage(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("This is a golang project")

	p := m.GetProfile()
	if p.PreferredLanguages.Value != "Go" {
		t.Errorf("expected 'Go', got %q", p.PreferredLanguages.Value)
	}
}

func TestAnalyze_ToolPreference(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("I use vim for all my editing")

	p := m.GetProfile()
	if p.ToolPreferences.Value != "vim" {
		t.Errorf("expected 'vim', got %q", p.ToolPreferences.Value)
	}
}

func TestAnalyze_ExpertiseLevel(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("I'm a senior developer with 10 years experience")

	p := m.GetProfile()
	if p.TechnicalLevel.Value != "senior" {
		t.Errorf("expected 'senior', got %q", p.TechnicalLevel.Value)
	}
}

func TestAnalyze_BeginnerLevel(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("I'm a beginner programmer just starting out")

	p := m.GetProfile()
	if p.TechnicalLevel.Value != "beginner" {
		t.Errorf("expected 'beginner', got %q", p.TechnicalLevel.Value)
	}
}

func TestAnalyze_EmptyMessage(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("")
	c.Analyze("   ")

	p := m.GetProfile()
	if p.PreferredLanguages.Value != "" {
		t.Error("expected no updates for empty messages")
	}
}

func TestAnalyze_RateLimiting(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	// First update should succeed
	c.Analyze("I love Python")
	p := m.GetProfile()
	if p.PreferredLanguages.Value != "Python" {
		t.Fatalf("expected 'Python', got %q", p.PreferredLanguages.Value)
	}

	// Second update to same dimension should be rate-limited
	c.Analyze("Actually I prefer JavaScript")
	p = m.GetProfile()
	if p.PreferredLanguages.Value != "Python" {
		t.Errorf("expected 'Python' (rate limited), got %q", p.PreferredLanguages.Value)
	}
}

func TestAnalyze_DifferentDimensionsNotRateLimited(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("I use Python and vim")

	p := m.GetProfile()
	if p.PreferredLanguages.Value != "Python" {
		t.Errorf("expected 'Python', got %q", p.PreferredLanguages.Value)
	}
	if p.ToolPreferences.Value != "vim" {
		t.Errorf("expected 'vim', got %q", p.ToolPreferences.Value)
	}
}

func TestAnalyze_AmbiguousSignalQueued(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("I prefer to work with microservices architecture")

	c.mu.Lock()
	queueLen := len(c.batchQueue)
	c.mu.Unlock()

	if queueLen != 1 {
		t.Errorf("expected 1 queued observation, got %d", queueLen)
	}
}

func TestFlushBatch_EmptyQueue(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	err := c.FlushBatch(func(s string) (string, error) {
		t.Error("summarize should not be called for empty queue")
		return "", nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestFlushBatch_ProcessesQueue(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	// Queue some observations
	c.mu.Lock()
	c.batchQueue = append(c.batchQueue, pendingObservation{
		Message:   "I prefer functional programming patterns",
		Timestamp: time.Now(),
	})
	c.batchQueue = append(c.batchQueue, pendingObservation{
		Message:   "My workflow involves TDD",
		Timestamp: time.Now(),
	})
	c.mu.Unlock()

	called := false
	err := c.FlushBatch(func(text string) (string, error) {
		called = true
		if text == "" {
			t.Error("expected non-empty text")
		}
		// Return structured updates
		return "work_patterns:TDD practitioner", nil
	})

	if err != nil {
		t.Fatalf("FlushBatch error: %v", err)
	}
	if !called {
		t.Error("summarize callback was not called")
	}

	p := m.GetProfile()
	if p.WorkPatterns.Value != "TDD practitioner" {
		t.Errorf("expected 'TDD practitioner', got %q", p.WorkPatterns.Value)
	}

	// Queue should be cleared
	c.mu.Lock()
	if len(c.batchQueue) != 0 {
		t.Error("expected empty queue after flush")
	}
	c.mu.Unlock()
}

func TestFlushBatch_SummarizeError(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.mu.Lock()
	c.batchQueue = append(c.batchQueue, pendingObservation{
		Message:   "some observation",
		Timestamp: time.Now(),
	})
	c.mu.Unlock()

	err := c.FlushBatch(func(s string) (string, error) {
		return "", fmt.Errorf("LLM timeout")
	})

	if err == nil {
		t.Error("expected error from FlushBatch")
	}

	// Queue should still be cleared (observations are discarded on error)
	c.mu.Lock()
	if len(c.batchQueue) != 0 {
		t.Error("expected empty queue after failed flush")
	}
	c.mu.Unlock()
}

func TestFlushBatch_RespectsRateLimit(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	// First, do a direct update to preferred_languages
	c.Analyze("I use Python")

	// Now queue an observation and flush with a result that tries to update the same dimension
	c.mu.Lock()
	c.batchQueue = append(c.batchQueue, pendingObservation{
		Message:   "some observation",
		Timestamp: time.Now(),
	})
	c.mu.Unlock()

	c.FlushBatch(func(s string) (string, error) {
		return "preferred_languages:JavaScript", nil
	})

	// Should still be Python due to rate limiting
	p := m.GetProfile()
	if p.PreferredLanguages.Value != "Python" {
		t.Errorf("expected 'Python' (rate limited), got %q", p.PreferredLanguages.Value)
	}
}

func TestResetSession(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	// Do an update
	c.Analyze("I use Python")
	p := m.GetProfile()
	if p.PreferredLanguages.Value != "Python" {
		t.Fatal("setup failed")
	}

	// Queue something
	c.mu.Lock()
	c.batchQueue = append(c.batchQueue, pendingObservation{Message: "test", Timestamp: time.Now()})
	c.mu.Unlock()

	// Reset session
	c.ResetSession()

	// Verify counts are cleared
	c.mu.Lock()
	if len(c.updateCounts) != 0 {
		t.Error("expected empty update counts after reset")
	}
	if len(c.batchQueue) != 0 {
		t.Error("expected empty batch queue after reset")
	}
	c.mu.Unlock()

	// Now we should be able to update the same dimension again
	c.Analyze("Actually I prefer JavaScript")
	p = m.GetProfile()
	if p.PreferredLanguages.Value != "JavaScript" {
		t.Errorf("expected 'JavaScript' after session reset, got %q", p.PreferredLanguages.Value)
	}
}

func TestAnalyze_NoMatchNoUpdate(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("Hello, how are you today?")

	p := m.GetProfile()
	if p.PreferredLanguages.Value != "" {
		t.Error("expected no language update")
	}
	if p.ToolPreferences.Value != "" {
		t.Error("expected no tool update")
	}
	if p.TechnicalLevel.Value != "" {
		t.Error("expected no expertise update")
	}
}

func TestAnalyze_Docker(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("I deploy everything with docker and kubernetes")

	p := m.GetProfile()
	if p.ToolPreferences.Value != "docker" {
		t.Errorf("expected 'docker', got %q", p.ToolPreferences.Value)
	}
}

func TestAnalyze_VSCode(t *testing.T) {
	m, _ := NewModel("/tmp/nonexistent.json")
	c := NewCollector(m)

	c.Analyze("I use vscode as my primary editor")

	p := m.GetProfile()
	if p.ToolPreferences.Value != "vscode" {
		t.Errorf("expected 'vscode', got %q", p.ToolPreferences.Value)
	}
}
