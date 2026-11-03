package agent

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// VisualPreviewImageMarker prefixes a user message that carries a rendered
// page or slide. It is distinct from the computer-use screenshot marker so
// the two channels do not prune each other.
const VisualPreviewImageMarker = "[visual_preview]"

func splitVisualPreviewImages(images []ToolModelImage) (desktop, preview []ToolModelImage) {
	for _, img := range images {
		if img.Purpose == VisualPreviewImagePurpose {
			preview = append(preview, img)
			continue
		}
		desktop = append(desktop, img)
	}
	return desktop, preview
}

// applyLatestDesktopShot updates the desktop finish-check image.
// A visual preview must not become that image, and must not clear one that
// a previous desktop action already captured.
func applyLatestDesktopShot(latest *ToolModelImage, toolName string, desktop, preview []ToolModelImage) *ToolModelImage {
	if len(desktop) > 0 {
		shot := desktop[len(desktop)-1]
		return &shot
	}
	if len(preview) == 0 && strings.TrimSpace(toolName) == "desktop" {
		return nil
	}
	return latest
}

func appendVisualPreviewImages(conversation []interface{}, cfg corelib.MaclawLLMConfig, images []ToolModelImage) []interface{} {
	if len(images) == 0 || !cfg.SupportsVision {
		return conversation
	}
	conversation = pruneVisualPreviewImages(conversation)
	return append(conversation, buildVisualPreviewMessage(cfg.Protocol, images))
}

func pruneVisualPreviewImages(conversation []interface{}) []interface{} {
	return keepNewestMarkedVisionImages(conversation, VisualPreviewImageMarker, ComputerUseKeepImages()-1, VisualPreviewImageMarker+" previous preview omitted; use the latest preview image.")
}

func buildVisualPreviewMessage(protocol string, images []ToolModelImage) map[string]interface{} {
	atts := make([]MessageAttachment, 0, len(images))
	for _, img := range images {
		mime := strings.TrimSpace(img.MIME)
		if mime == "" {
			mime = "image/png"
		}
		atts = append(atts, MessageAttachment{MimeType: mime, Data: img.Base64})
	}
	text := VisualPreviewImageMarker + " Rendered page or slide preview. Judge layout, overflow, overlap, spacing, color, and whether styles applied. Coordinates are not desktop clicks."
	for _, img := range images {
		if caption := strings.TrimSpace(img.Caption); caption != "" {
			text += "\n" + caption
			break
		}
	}
	var content interface{}
	if strings.EqualFold(strings.TrimSpace(protocol), "anthropic") {
		content = BuildAnthropicVisionContent(text, atts)
	} else {
		content = BuildOpenAIVisionContent(text, atts)
	}
	return map[string]interface{}{"role": "user", "content": content}
}

func keepNewestMarkedVisionImages(conversation []interface{}, marker string, keep int, omitted string) []interface{} {
	var idx []int
	for i, raw := range conversation {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role != "user" {
			continue
		}
		blocks, ok := msg["content"].([]interface{})
		if !ok || !markedVisionMessage(blocks, marker) {
			continue
		}
		idx = append(idx, i)
	}
	if len(idx) <= keep {
		return conversation
	}
	kept := map[int]bool{}
	if keep > 0 {
		for _, i := range idx[len(idx)-keep:] {
			kept[i] = true
		}
	}
	out := make([]interface{}, 0, len(conversation))
	for i, raw := range conversation {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			out = append(out, raw)
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			out = append(out, raw)
			continue
		}
		blocks, ok := msg["content"].([]interface{})
		if !ok || !markedVisionMessage(blocks, marker) || kept[i] {
			out = append(out, raw)
			continue
		}
		out = append(out, map[string]interface{}{
			"role":    "user",
			"content": omitted,
		})
	}
	return out
}

func markedVisionMessage(blocks []interface{}, marker string) bool {
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := block["type"].(string); t == "text" {
			if text, _ := block["text"].(string); strings.Contains(text, marker) {
				return true
			}
		}
	}
	return false
}
