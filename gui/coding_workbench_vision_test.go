package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestHasCodingImageAttachment(t *testing.T) {
	if hasCodingImageAttachment(nil) {
		t.Fatal("nil")
	}
	if hasCodingImageAttachment([]agent.MessageAttachment{{Type: "file", MimeType: "text/plain"}}) {
		t.Fatal("plain file")
	}
	if !hasCodingImageAttachment([]agent.MessageAttachment{{Type: "image", MimeType: "image/png"}}) {
		t.Fatal("image type")
	}
	if !hasCodingImageAttachment([]agent.MessageAttachment{{MimeType: "image/jpeg"}}) {
		t.Fatal("jpeg mime")
	}
}

func TestShouldUseRemoteCodingIsolate(t *testing.T) {
	if shouldUseRemoteCodingIsolate(codingWorktreeModeOff, true, "implement", "write", nil) {
		t.Fatal("off")
	}
	if shouldUseRemoteCodingIsolate(codingWorktreeModeAuto, true, "explore", "探查代码", nil) {
		t.Fatal("explore")
	}
	if !shouldUseRemoteCodingIsolate(codingWorktreeModeAuto, true, "implement JWT", "写代码", nil) {
		t.Fatal("auto planned independent implement")
	}
	if shouldUseRemoteCodingIsolate(codingWorktreeModeAuto, true, "implement JWT", "写代码", []int{1}) {
		t.Fatal("auto chained should not isolate")
	}
	if !shouldUseRemoteCodingIsolate(codingWorktreeModeAlways, false, "implement", "x", []int{1}) {
		t.Fatal("always")
	}
}

func TestRecordStickyCodingRoute(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:route-test"
	h.recordStickyCodingRoute(userID, "gpt-4o", "route", "vision", "has image")
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.LastRouteModel != "gpt-4o" || !strings.Contains(mem.LastRouteReason, "image") {
		t.Fatalf("%+v", mem)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestCodingRouteCapabilitiesWithoutRouter(t *testing.T) {
	invalidateCodingRouteCapabilitiesCache()
	h := &IMMessageHandler{}
	caps := h.codingRouteCapabilities()
	if len(caps) != 4 {
		t.Fatalf("%+v", caps)
	}
	byPref := map[string]codingRouteCapability{}
	for _, c := range caps {
		byPref[c.Pref] = c
	}
	if !byPref[codingRoutePrefPrimary].Available {
		t.Fatal("primary always available")
	}
	if !strings.Contains(byPref[codingRoutePrefReasoning].Note, "reasoning") &&
		byPref[codingRoutePrefReasoning].Source != "primary" {
		// without router: note or primary source
		t.Logf("reasoning cap=%+v", byPref[codingRoutePrefReasoning])
	}
	// Cache hit returns equal length.
	caps2 := h.codingRouteCapabilities()
	if len(caps2) != 4 {
		t.Fatalf("cache %+v", caps2)
	}
	invalidateCodingRouteCapabilitiesCache()
	md := h.formatCodingRouteCapabilitiesMarkdown()
	if !strings.Contains(md, "ModelRouter") || !strings.Contains(md, "reasoning") {
		t.Fatalf("md=%s", md)
	}
}

func TestCodingSubAgentUserContentNoAttachments(t *testing.T) {
	sa := &CodingSubAgent{}
	got := codingSubAgentUserContent(sa, "hello")
	if got != "hello" {
		t.Fatalf("%v", got)
	}
}

func TestCodingSubAgentUserContentWithImageSavesWhenNoVision(t *testing.T) {
	sa := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{Protocol: "openai", SupportsVision: false}, nil, "", nil)
	sa.SetAttachments([]agent.MessageAttachment{{
		Type:     "image",
		FileName: "shot.png",
		MimeType: "image/png",
		// 1x1 transparent PNG
		Data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	}})
	got := codingSubAgentUserContent(sa, "implement this UI")
	// Without vision support, BuildUserContent returns a string with saved path note.
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected string content when vision unsupported, got %T", got)
	}
	if !strings.Contains(s, "implement this UI") {
		t.Fatalf("missing user text: %q", s)
	}
	if !strings.Contains(s, "图片") && !strings.Contains(strings.ToLower(s), "image") {
		t.Fatalf("expected image note: %q", s)
	}
}

func TestCodingSubAgentUserContentWithVisionMultimodal(t *testing.T) {
	sa := NewCodingSubAgent(nil, corelib.MaclawLLMConfig{Protocol: "openai", SupportsVision: true}, nil, "", nil)
	sa.SetAttachments([]agent.MessageAttachment{{
		Type:     "image",
		FileName: "shot.png",
		MimeType: "image/png",
		Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	}})
	got := codingSubAgentUserContent(sa, "fix layout")
	// Multimodal content is typically []map or []interface{}.
	if _, ok := got.(string); ok {
		t.Fatalf("expected multimodal structure when vision enabled, got string %q", got)
	}
}
