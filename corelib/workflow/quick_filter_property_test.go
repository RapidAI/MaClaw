package workflow

import (
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func quickConfig() *quick.Config {
	return &quick.Config{MaxCount: 100}
}

// MockWorkflowChecker is a test double for WorkflowChecker.
type MockWorkflowChecker struct {
	ActiveWorkflow      map[string]bool
	ActiveUnderstanding map[string]bool
}

func (m *MockWorkflowChecker) HasActiveWorkflow(userID string) bool {
	if m.ActiveWorkflow == nil {
		return false
	}
	return m.ActiveWorkflow[userID]
}

func (m *MockWorkflowChecker) HasActiveUnderstanding(userID string) bool {
	if m.ActiveUnderstanding == nil {
		return false
	}
	return m.ActiveUnderstanding[userID]
}

func TestProperty1_ActiveSessionRoutingPriority(t *testing.T) {
	f1 := func(userID string, text string) bool {
		if userID == "" {
			return true
		}
		checker := &MockWorkflowChecker{
			ActiveWorkflow: map[string]bool{userID: true},
		}
		qf := NewQuickFilter(checker)
		return qf.Classify(userID, text) == FilterActiveWorkflow
	}
	if err := quick.Check(f1, quickConfig()); err != nil {
		t.Errorf("active workflow priority failed: %v", err)
	}

	f2 := func(userID string, text string) bool {
		if userID == "" {
			return true
		}
		checker := &MockWorkflowChecker{
			ActiveWorkflow:      map[string]bool{userID: false},
			ActiveUnderstanding: map[string]bool{userID: true},
		}
		qf := NewQuickFilter(checker)
		return qf.Classify(userID, text) == FilterActiveUnderstanding
	}
	if err := quick.Check(f2, quickConfig()); err != nil {
		t.Errorf("active understanding priority failed: %v", err)
	}
}

func TestProperty2_FreeFormInputAlwaysRequiresUnderstanding(t *testing.T) {
	qf := NewQuickFilter(&MockWorkflowChecker{})

	f := func(text string) bool {
		result := qf.Classify("user1", text)
		if strings.TrimSpace(text) == "" {
			return result == FilterSimpleDirective
		}
		return result == FilterNeedsUnderstanding
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("free-form routing property failed: %v", err)
	}
}

func TestProperty3_PerformanceGuarantee(t *testing.T) {
	qf := NewQuickFilter(&MockWorkflowChecker{})

	lengths := []int{0, 10, 100, 500, 1000, 5000, 10000}
	testStrings := make([]string, len(lengths))
	rng := rand.New(rand.NewSource(42))
	for idx, length := range lengths {
		runes := make([]rune, length)
		for i := range runes {
			if rng.Intn(2) == 0 {
				runes[i] = rune(rng.Intn(94) + 33)
			} else {
				runes[i] = rune(rng.Intn(0x9FFF-0x4E00) + 0x4E00)
			}
		}
		testStrings[idx] = string(runes)
	}

	for _, s := range testStrings {
		qf.Classify("warmup", s)
	}

	f := func(idx uint8) bool {
		text := testStrings[int(idx)%len(testStrings)]
		start := time.Now()
		qf.Classify("perfUser", text)
		return time.Since(start) < 5*time.Millisecond
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("performance <5ms failed: %v", err)
	}
}
