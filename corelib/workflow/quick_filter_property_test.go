package workflow

import (
	"math/rand"
	"testing"
	"testing/quick"
	"time"
	"unicode/utf8"
)

// quickConfig returns a shared quick.Config for all property tests.
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

// Feature: maclaw-agent-workflow, Property 1: 活跃会话路由优先级
// For any user ID and any message text, when the user has an active workflow,
// Classify returns active_workflow; when the user has an active understanding
// session (and no active workflow), returns active_understanding regardless of
// message content.
// **Validates: Requirements 1.3, 1.4**
func TestProperty1_ActiveSessionRoutingPriority(t *testing.T) {
	// Sub-property 1a: active workflow always returns active_workflow
	f1 := func(userID string, text string) bool {
		if userID == "" {
			return true // skip empty userID
		}
		checker := &MockWorkflowChecker{
			ActiveWorkflow: map[string]bool{userID: true},
		}
		qf := NewQuickFilter(checker)
		result := qf.Classify(userID, text)
		return result == FilterActiveWorkflow
	}
	if err := quick.Check(f1, quickConfig()); err != nil {
		t.Errorf("Property 1a (active workflow priority) failed: %v", err)
	}

	// Sub-property 1b: active understanding (no workflow) returns active_understanding
	f2 := func(userID string, text string) bool {
		if userID == "" {
			return true
		}
		checker := &MockWorkflowChecker{
			ActiveWorkflow:      map[string]bool{userID: false},
			ActiveUnderstanding: map[string]bool{userID: true},
		}
		qf := NewQuickFilter(checker)
		result := qf.Classify(userID, text)
		return result == FilterActiveUnderstanding
	}
	if err := quick.Check(f2, quickConfig()); err != nil {
		t.Errorf("Property 1b (active understanding priority) failed: %v", err)
	}
}

// Feature: maclaw-agent-workflow, Property 2: QuickFilter 分类正确性
// For any user without active sessions, small_talk/simple_directive/needs_understanding
// patterns match correctly.
// **Validates: Requirements 1.1, 1.2, 1.6**
func TestProperty2_ClassificationCorrectness(t *testing.T) {
	noSessionChecker := &MockWorkflowChecker{}
	qf := NewQuickFilter(noSessionChecker)

	// 2a: small talk messages (short + greeting word) → small_talk
	smallTalkSamples := []string{
		"你好", "谢谢", "早上好", "hi", "hello", "bye", "好的", "嗯",
	}
	f1 := func(idx uint8) bool {
		i := int(idx) % len(smallTalkSamples)
		msg := smallTalkSamples[i]
		result := qf.Classify("user1", msg)
		return result == FilterSmallTalk
	}
	if err := quick.Check(f1, quickConfig()); err != nil {
		t.Errorf("Property 2a (small talk) failed: %v", err)
	}

	// 2b: In the new architecture, ALL non-small-talk messages go to LLM
	// (FilterNeedsUnderstanding). Simple directives are no longer classified
	// at the QuickFilter level — the LLM decides.
	directiveSamples := []string{
		"翻译这段话", "帮我翻译一下", "总结这篇文章", "格式化代码",
		"帮我搜一下", "计算123+456",
	}
	f2 := func(idx uint8) bool {
		i := int(idx) % len(directiveSamples)
		msg := directiveSamples[i]
		result := qf.Classify("user1", msg)
		return result == FilterNeedsUnderstanding
	}
	if err := quick.Check(f2, quickConfig()); err != nil {
		t.Errorf("Property 2b (non-small-talk → LLM) failed: %v", err)
	}

	// 2c: complex task (verb + target + constraint) → needs_understanding
	complexSamples := []string{
		"帮我开发一个CRM系统，需要支持多租户",
		"设计一个电商平台，要求高可用",
		"帮我创建一个项目管理工具，需要权限控制",
		"帮我搭建一个微服务系统，必须支持高并发",
	}
	f3 := func(idx uint8) bool {
		i := int(idx) % len(complexSamples)
		msg := complexSamples[i]
		result := qf.Classify("user1", msg)
		return result == FilterNeedsUnderstanding
	}
	if err := quick.Check(f3, quickConfig()); err != nil {
		t.Errorf("Property 2c (needs understanding) failed: %v", err)
	}
}

// Feature: maclaw-agent-workflow, Property 3: QuickFilter 性能保证
// For any message text of 0-10000 characters, Classify completes in <5ms.
// **Validates: Requirements 1.5, 13.1**
func TestProperty3_PerformanceGuarantee(t *testing.T) {
	noSessionChecker := &MockWorkflowChecker{}
	qf := NewQuickFilter(noSessionChecker)

	// Pre-generate test strings of various lengths to avoid allocation in timing loop
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

	// Warm up to avoid first-call overhead
	for _, s := range testStrings {
		qf.Classify("warmup", s)
	}

	f := func(idx uint8) bool {
		text := testStrings[int(idx)%len(testStrings)]
		_ = utf8.RuneCountInString(text) // ensure rune count is valid

		start := time.Now()
		qf.Classify("perfUser", text)
		elapsed := time.Since(start)

		return elapsed < 5*time.Millisecond
	}
	if err := quick.Check(f, quickConfig()); err != nil {
		t.Errorf("Property 3 (performance <5ms) failed: %v", err)
	}
}
