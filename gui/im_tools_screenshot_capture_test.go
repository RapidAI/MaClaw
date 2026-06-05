package main

import "testing"

func TestShouldReturnScreenshotBase64ForPlatform(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		want     bool
	}{
		{name: "unknown defaults to desktop playback", platform: "", want: false},
		{name: "desktop uses playback", platform: "desktop", want: false},
		{name: "tui uses playback", platform: "tui", want: false},
		{name: "weixin returns inline base64", platform: "weixin", want: true},
		{name: "qqbot local returns inline base64", platform: "qqbot_local", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldReturnScreenshotBase64ForPlatform(tt.platform); got != tt.want {
				t.Fatalf("shouldReturnScreenshotBase64ForPlatform(%q) = %v, want %v", tt.platform, got, tt.want)
			}
		})
	}
}

func TestCurrentRuntimePlatformDoesNotReadGlobalLoop(t *testing.T) {
	h := &IMMessageHandler{currentLoopCtx: &LoopContext{Platform: "weixin"}}
	if got := h.currentRuntimePlatform(); got != "" {
		t.Fatalf("currentRuntimePlatform() = %q, want empty isolation boundary", got)
	}
}

func TestConsumeRuntimePlatformFromToolArgsOverridesCurrentLoopPlatform(t *testing.T) {
	h := &IMMessageHandler{currentLoopCtx: &LoopContext{Platform: "desktop"}}
	args := map[string]interface{}{registeredToolRuntimePlatformField: "weixin"}
	if got := h.consumeRuntimePlatformFromToolArgsOrCurrent(args); got != "weixin" {
		t.Fatalf("runtime platform = %q, want weixin", got)
	}
	if _, ok := args[registeredToolRuntimePlatformField]; ok {
		t.Fatal("runtime platform hidden field should be consumed")
	}
	if got := shouldReturnScreenshotBase64ForPlatform("weixin"); !got {
		t.Fatal("weixin screenshot should return inline base64")
	}
}

func TestRuntimePlatformForExplicitOwnerDoesNotInheritCurrentLoop(t *testing.T) {
	h := &IMMessageHandler{currentLoopCtx: &LoopContext{Platform: "weixin", Runtime: RuntimeContext{RequestID: "req-other", PolicyOwnerID: "other"}}}
	if got := h.runtimePlatformForOwnerOrCurrent("owner-without-loop", true); got != "" {
		t.Fatalf("explicit owner without loop platform = %q, want empty (no global inheritance)", got)
	}
}

func TestRuntimePlatformForOwnerUsesOwnerLoop(t *testing.T) {
	h := &IMMessageHandler{currentLoopCtx: &LoopContext{Platform: "weixin", Runtime: RuntimeContext{RequestID: "req-other", PolicyOwnerID: "other"}}}
	h.setSessionLoopCtx("owner-1", &LoopContext{Platform: "desktop", Runtime: RuntimeContext{RequestID: "req-owner", PolicyOwnerID: "owner-1"}})
	if got := h.runtimePlatformForOwnerOrCurrent("owner-1", true); got != "desktop" {
		t.Fatalf("owner platform = %q, want desktop", got)
	}
}
