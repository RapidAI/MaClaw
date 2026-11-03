package agent

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestVisualPreviewDoesNotReplaceDesktopShot(t *testing.T) {
	desktop := &ToolModelImage{MIME: "image/png", Base64: "desk"}
	preview := []ToolModelImage{{MIME: "image/png", Base64: "page", Purpose: VisualPreviewImagePurpose}}
	desktopImages, previewImages := splitVisualPreviewImages(preview)
	if len(desktopImages) != 0 || len(previewImages) != 1 {
		t.Fatalf("split desktop=%d preview=%d", len(desktopImages), len(previewImages))
	}
	got := applyLatestDesktopShot(desktop, "write_file", desktopImages, previewImages)
	if got == nil || got.Base64 != "desk" {
		t.Fatalf("preview replaced desktop shot: %#v", got)
	}
	cleared := applyLatestDesktopShot(desktop, "desktop", nil, nil)
	if cleared != nil {
		t.Fatalf("desktop tool without an image kept the old shot")
	}
	kept := applyLatestDesktopShot(desktop, "desktop", nil, previewImages)
	if kept == nil || kept.Base64 != "desk" {
		t.Fatalf("preview images cleared the desktop shot: %#v", kept)
	}
	live := applyLatestDesktopShot(nil, "desktop", []ToolModelImage{{Base64: "live"}}, nil)
	if live == nil || live.Base64 != "live" {
		t.Fatalf("desktop image did not become the shot: %#v", live)
	}
}

func TestVisualPreviewMessageKeepsPageCaption(t *testing.T) {
	msg := buildVisualPreviewMessage("openai", []ToolModelImage{{
		MIME: "image/png", Base64: "aaaa", Purpose: VisualPreviewImagePurpose,
		Caption: "预览图已附上：第 4-6 页 / 共 9 页。",
	}})
	blocks, _ := msg["content"].([]interface{})
	if len(blocks) == 0 {
		t.Fatalf("content = %#v", msg["content"])
	}
	text, _ := blocks[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "第 4-6 页") || !strings.Contains(text, VisualPreviewImageMarker) {
		t.Fatalf("caption missing: %q", text)
	}
}

func TestAppendVisualPreviewSkipsTextModels(t *testing.T) {
	start := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	got := appendVisualPreviewImages(start, corelib.MaclawLLMConfig{SupportsVision: false}, []ToolModelImage{{
		MIME: "image/png", Base64: "aaaa", Purpose: VisualPreviewImagePurpose,
	}})
	if len(got) != 1 {
		t.Fatalf("text model kept a preview image: %#v", got)
	}
}
