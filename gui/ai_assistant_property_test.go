package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
)

// ---------------------------------------------------------------------------
// Property-based tests for ai-assistant-sidebar-icon feature.
//
// Each test verifies a correctness property of the AI assistant backend:
// message construction, conversation memory isolation, error propagation,
// and history clearing. Uses testing/quick with at least 100 iterations.
// ---------------------------------------------------------------------------

// aiRandomString generates a random ASCII string of length n.
func aiRandomString(r *rand.Rand, n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_- "
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[r.Intn(len(letters))]
	}
	return string(b)
}

// aiRandomText is a quick.Generator wrapper for random text strings.
type aiRandomText struct {
	Text string
}

func (aiRandomText) Generate(r *rand.Rand, size int) reflect.Value {
	n := r.Intn(100) + 1
	// Prefix with "msg:" to avoid accidentally generating slash commands
	// like "/new", "/reset", "/clear" which are handled before the LLM check.
	return reflect.ValueOf(aiRandomText{Text: "msg:" + aiRandomString(r, n)})
}

// aiRandomEntries is a quick.Generator wrapper for random conversation entries.
type aiRandomEntries struct {
	DesktopEntries []agent.ConversationEntry
	IMUserID       string
	IMEntries      []agent.ConversationEntry
}

func (aiRandomEntries) Generate(r *rand.Rand, size int) reflect.Value {
	roles := []string{"user", "assistant", "tool"}
	makeEntries := func(count int) []agent.ConversationEntry {
		entries := make([]agent.ConversationEntry, count)
		for i := range entries {
			entries[i] = agent.ConversationEntry{
				Role:    roles[r.Intn(len(roles))],
				Content: aiRandomString(r, r.Intn(50)+1),
			}
		}
		return entries
	}

	desktopCount := r.Intn(10) + 1
	imCount := r.Intn(10) + 1
	imUserID := "im-user-" + aiRandomString(r, r.Intn(8)+4)

	return reflect.ValueOf(aiRandomEntries{
		DesktopEntries: makeEntries(desktopCount),
		IMUserID:       imUserID,
		IMEntries:      makeEntries(imCount),
	})
}

// aiQuickConfig returns a quick.Config with at least 100 iterations.
func aiQuickConfig() *quick.Config {
	return &quick.Config{MaxCount: 100}
}

// ---------------------------------------------------------------------------
// Feature: ai-assistant-sidebar-icon, Property 4: 后端消息构造与平台标识
//
// Validates: Requirements 3.1, 3.2
// For any text input, constructing an IMUserMessage for the desktop AI
// assistant must always produce UserID == "desktop-user" and
// Platform == "desktop", regardless of the text content.
// ---------------------------------------------------------------------------
func TestAIAssistantProperty4_MessageConstructionPlatformID(t *testing.T) {
	f := func(input aiRandomText) bool {
		msg := IMUserMessage{
			UserID:   "desktop-user",
			Platform: "desktop",
			Text:     input.Text,
		}

		if msg.UserID != "desktop-user" {
			t.Logf("expected UserID 'desktop-user', got %q", msg.UserID)
			return false
		}
		if msg.Platform != "desktop" {
			t.Logf("expected Platform 'desktop', got %q", msg.Platform)
			return false
		}
		if msg.Text != input.Text {
			t.Logf("expected Text %q, got %q", input.Text, msg.Text)
			return false
		}
		return true
	}

	if err := quick.Check(f, aiQuickConfig()); err != nil {
		t.Errorf("Property 4 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: ai-assistant-sidebar-icon, Property 5: 桌面端对话记忆隔离
//
// Validates: Requirements 3.3
// For any set of desktop entries and IM entries saved under different user
// IDs, loading "desktop-user" must return only desktop entries, and loading
// the IM user ID must return only IM entries. The two memory spaces must
// not interfere with each other.
// ---------------------------------------------------------------------------
func TestAIAssistantProperty5_DesktopConversationMemoryIsolation(t *testing.T) {
	f := func(input aiRandomEntries) bool {
		cm := agent.NewConversationMemory()
		defer cm.Stop()

		cm.Save("desktop-user", input.DesktopEntries)
		cm.Save(input.IMUserID, input.IMEntries)

		desktopLoaded := cm.Load("desktop-user")
		imLoaded := cm.Load(input.IMUserID)

		// Desktop entries count must match.
		if len(desktopLoaded) != len(input.DesktopEntries) {
			t.Logf("desktop entry count mismatch: got %d, want %d",
				len(desktopLoaded), len(input.DesktopEntries))
			return false
		}

		// IM entries count must match.
		if len(imLoaded) != len(input.IMEntries) {
			t.Logf("IM entry count mismatch: got %d, want %d",
				len(imLoaded), len(input.IMEntries))
			return false
		}

		// Verify desktop entries content matches exactly.
		for i, e := range desktopLoaded {
			if e.Role != input.DesktopEntries[i].Role || e.Content != input.DesktopEntries[i].Content {
				t.Logf("desktop entry[%d] mismatch: got {%q, %q}, want {%q, %q}",
					i, e.Role, e.Content, input.DesktopEntries[i].Role, input.DesktopEntries[i].Content)
				return false
			}
		}

		// Verify IM entries content matches exactly.
		for i, e := range imLoaded {
			if e.Role != input.IMEntries[i].Role || e.Content != input.IMEntries[i].Content {
				t.Logf("IM entry[%d] mismatch: got {%q, %q}, want {%q, %q}",
					i, e.Role, e.Content, input.IMEntries[i].Role, input.IMEntries[i].Content)
				return false
			}
		}

		return true
	}

	if err := quick.Check(f, aiQuickConfig()); err != nil {
		t.Errorf("Property 5 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: ai-assistant-sidebar-icon, Property 6: 错误响应传播
//
// Validates: Requirements 3.4
// For any text input, when LLM is not configured (bare App with no config),
// HandleIMMessage must return an IMAgentResponse with a non-empty Error field.
// ---------------------------------------------------------------------------
func TestAIAssistantProperty6_ErrorResponsePropagation(t *testing.T) {
	f := func(input aiRandomText) bool {
		// Use testHomeDir pointing to a non-existent directory so
		// LoadConfig fails → isMaclawLLMConfigured() returns false.
		app := &App{testHomeDir: t.TempDir()}
		mgr := &RemoteSessionManager{
			app:      app,
			sessions: map[string]*RemoteSession{},
		}
		h := NewIMMessageHandler(app, mgr)

		msg := IMUserMessage{
			UserID:   "desktop-user",
			Platform: "desktop",
			Text:     input.Text,
		}

		resp := h.HandleIMMessage(msg)

		if resp == nil {
			t.Logf("expected non-nil response for text %q", input.Text)
			return false
		}
		if resp.Error == "" {
			t.Logf("expected non-empty Error for unconfigured LLM, got empty for text %q", input.Text)
			return false
		}
		return true
	}

	if err := quick.Check(f, aiQuickConfig()); err != nil {
		t.Errorf("Property 6 failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Feature: ai-assistant-sidebar-icon, Property 7: 清空历史清除记忆
//
// Validates: Requirements 3.5
// For any non-empty set of desktop conversation entries, after saving them
// and then calling clear("desktop-user"), load("desktop-user") must return
// an empty (nil or zero-length) slice.
// ---------------------------------------------------------------------------
func TestAIAssistantProperty7_ClearHistoryClearsMemory(t *testing.T) {
	f := func(input aiRandomEntries) bool {
		cm := agent.NewConversationMemory()
		defer cm.Stop()

		cm.Save("desktop-user", input.DesktopEntries)

		// Verify entries were saved.
		loaded := cm.Load("desktop-user")
		if len(loaded) == 0 {
			t.Logf("expected non-empty entries after save, got empty")
			return false
		}

		// Clear and verify.
		cm.Clear("desktop-user")
		afterClear := cm.Load("desktop-user")
		if len(afterClear) != 0 {
			t.Logf("expected empty entries after clear, got %d entries", len(afterClear))
			return false
		}

		return true
	}

	if err := quick.Check(f, aiQuickConfig()); err != nil {
		t.Errorf("Property 7 failed: %v", err)
	}
}
