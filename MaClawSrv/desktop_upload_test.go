package main

import (
	"strings"
	"testing"
)

func TestDesktopUploadFilesAndHook(t *testing.T) {
	got := desktopUploadFiles(map[string]any{"files": []any{"/a.png", " /b.jpg "}, "path": "/c.png"})
	if strings.Join(got, "|") != "/a.png|/b.jpg|/c.png" {
		t.Fatalf("files = %v", got)
	}
	if len(desktopUploadFiles(map[string]any{})) != 0 {
		t.Fatal("empty")
	}
	for _, want := range []string{"showPicker", "data-maclaw-upload", "shadowRoot", "appendChild"} {
		if !strings.Contains(desktopUploadHookJS, want) {
			t.Fatalf("hook misses %s", want)
		}
	}
	if !desktopActionAllowed("upload") || !desktopBatchStepActions["upload"] {
		t.Fatal("upload not exposed")
	}
}
