package guiapp

import (
	"strings"
	"testing"
	"time"
)

func TestPendingSteerInjectionNote_PeeksWithoutConsuming(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/note-demo"
	if note := h.pendingSteerInjectionNote(userID); note != "" {
		t.Fatalf("empty queue note = %q, want empty", note)
	}
	h.accumulateInjection(userID, buildGuideLaunchInjection("改成TUI界面，类似GUI的效果"))

	note := h.pendingSteerInjectionNote(userID)
	if !strings.Contains(note, "改成TUI界面，类似GUI的效果") {
		t.Fatalf("note missing steering text: %q", note)
	}
	if !strings.Contains(note, "尚未生效") {
		t.Fatalf("note missing not-applied hint: %q", note)
	}
	// Peek must not consume: the injection still applies to the next round.
	if _, ok := h.pendingInjection.Load(userID); !ok {
		t.Fatal("pendingSteerInjectionNote must not consume the queued injection")
	}
}

func TestPendingSteerInjectionNote_IncludesFreshPreLoopGuide(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:C:/tasks/note-preloop"
	h.pendingPreLoopGuide.Store(userID, &preLoopGuideEntry{Text: "支持中英文", CreatedAt: time.Now()})

	note := h.pendingSteerInjectionNote(userID)
	if !strings.Contains(note, "支持中英文") {
		t.Fatalf("note missing pre-loop guide text: %q", note)
	}
	if _, ok := h.pendingPreLoopGuide.Load(userID); !ok {
		t.Fatal("pendingSteerInjectionNote must not consume the pre-loop guide")
	}

	// Stale pre-loop guides are not mentioned.
	staleID := "desktop-user:C:/tasks/note-stale"
	h.pendingPreLoopGuide.Store(staleID, &preLoopGuideEntry{Text: "旧调整", CreatedAt: time.Now().Add(-2 * preLoopGuideMaxAge)})
	if note := h.pendingSteerInjectionNote(staleID); note != "" {
		t.Fatalf("stale pre-loop guide note = %q, want empty", note)
	}
}
