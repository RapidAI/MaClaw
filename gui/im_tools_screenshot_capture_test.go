package main

import "testing"

func TestShouldReturnScreenshotBase64ForCurrentPlatform(t *testing.T) {
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
			h := &IMMessageHandler{}
			if tt.platform != "" {
				h.currentLoopCtx = &LoopContext{Platform: tt.platform}
			}
			if got := h.shouldReturnScreenshotBase64ForCurrentPlatform(); got != tt.want {
				t.Fatalf("shouldReturnScreenshotBase64ForCurrentPlatform() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldReturnScreenshotBase64ForCurrentPlatformNilHandler(t *testing.T) {
	if got := (*IMMessageHandler)(nil).shouldReturnScreenshotBase64ForCurrentPlatform(); got {
		t.Fatal("nil handler should default to desktop playback behavior")
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
