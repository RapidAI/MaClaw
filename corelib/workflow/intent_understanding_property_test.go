package workflow

import (
	"testing"
	"testing/quick"
	"time"
)

// MockLLMCaller is a test double for LLMCaller.
type MockLLMCaller struct {
	Response string
	Err      error
}

func (m *MockLLMCaller) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	return m.Response, nil
}

// Feature: maclaw-agent-workflow, Property 4: 意图理解会话过期清理
// For any set of UnderstandingSessions, CleanupExpired removes sessions with
// UpdatedAt > 30 minutes ago and preserves recent ones.
// **Validates: Requirements 2.7**
func TestProperty4_IntentUnderstandingCleanupExpired(t *testing.T) {
	f := func(recentCount, expiredCount uint8) bool {
		rc := int(recentCount)%5 + 1 // 1-5 recent sessions
		ec := int(expiredCount)%5 + 1 // 1-5 expired sessions

		llm := &MockLLMCaller{Response: `{"intent":{"category":"coding","summary":"test"},"reply":"ok","ready":false}`}
		mgr := NewIntentUnderstandingManager(NullStore{}, llm, nil)

		now := time.Now()

		// Add recent sessions (should survive)
		for i := 0; i < rc; i++ {
			uid := "recent_" + string(rune('A'+i))
			sess := &UnderstandingSession{
				ID:        "iu-" + uid,
				UserID:    uid,
				State:     UnderstandingActive,
				UpdatedAt: now.Add(-10 * time.Minute), // 10 min ago — within 30 min
				CreatedAt: now.Add(-10 * time.Minute),
			}
			mgr.mu.Lock()
			mgr.sessions[uid] = sess
			mgr.mu.Unlock()
		}

		// Add expired sessions (should be removed)
		for i := 0; i < ec; i++ {
			uid := "expired_" + string(rune('A'+i))
			sess := &UnderstandingSession{
				ID:        "iu-" + uid,
				UserID:    uid,
				State:     UnderstandingActive,
				UpdatedAt: now.Add(-45 * time.Minute), // 45 min ago — beyond 30 min
				CreatedAt: now.Add(-45 * time.Minute),
			}
			mgr.mu.Lock()
			mgr.sessions[uid] = sess
			mgr.mu.Unlock()
		}

		mgr.CleanupExpired()

		// Verify recent sessions survived
		for i := 0; i < rc; i++ {
			uid := "recent_" + string(rune('A'+i))
			if !mgr.HasActiveSession(uid) {
				return false
			}
		}
		// Verify expired sessions removed
		for i := 0; i < ec; i++ {
			uid := "expired_" + string(rune('A'+i))
			if mgr.HasActiveSession(uid) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 4 (intent understanding cleanup) failed: %v", err)
	}
}
