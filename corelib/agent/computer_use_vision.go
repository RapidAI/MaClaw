package agent

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ComputerUseVisionImageMarker prefixes the user message that carries a
// computer_observe screenshot so later observes can drop stale images.
const ComputerUseVisionImageMarker = "[computer_observe screenshot]"

func appendComputerUseVisionImages(conversation []interface{}, cfg corelib.MaclawLLMConfig, images []ToolModelImage) []interface{} {
	if len(images) == 0 || !cfg.SupportsVision {
		return conversation
	}
	conversation = pruneComputerUseVisionImages(conversation)
	return append(conversation, buildComputerUseVisionMessage(cfg.Protocol, images))
}

func pruneComputerUseVisionImages(conversation []interface{}) []interface{} {
	return replaceComputerUseVisionImages(conversation, -1)
}

func pruneOlderComputerUseVisionImages(conversation []interface{}) []interface{} {
	last := -1
	for i, raw := range conversation {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		blocks, ok := msg["content"].([]interface{})
		if !ok || !computerUseVisionMessage(blocks) {
			continue
		}
		last = i
	}
	if last < 0 {
		return conversation
	}
	return replaceComputerUseVisionImages(conversation, last)
}

func replaceComputerUseVisionImages(conversation []interface{}, keep int) []interface{} {
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
		if !ok || !computerUseVisionMessage(blocks) {
			out = append(out, raw)
			continue
		}
		if i == keep {
			out = append(out, raw)
			continue
		}
		out = append(out, map[string]interface{}{
			"role":    "user",
			"content": ComputerUseVisionImageMarker + " previous screenshot omitted; use the latest observe image.",
		})
	}
	return out
}

func computerUseVisionMessage(blocks []interface{}) bool {
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := block["type"].(string); t == "text" {
			if text, _ := block["text"].(string); strings.Contains(text, ComputerUseVisionImageMarker) {
				return true
			}
		}
	}
	return false
}

func buildComputerUseVisionMessage(protocol string, images []ToolModelImage) map[string]interface{} {
	atts := make([]MessageAttachment, 0, len(images))
	for _, img := range images {
		mime := strings.TrimSpace(img.MIME)
		if mime == "" {
			mime = "image/png"
		}
		atts = append(atts, MessageAttachment{MimeType: mime, Data: img.Base64})
	}
	text := ComputerUseVisionImageMarker + " Look at this desktop screenshot. Click x,y in this image's pixel space (origin top-left)."
	var content interface{}
	if strings.EqualFold(strings.TrimSpace(protocol), "anthropic") {
		content = BuildAnthropicVisionContent(text, atts)
	} else {
		content = BuildOpenAIVisionContent(text, atts)
	}
	return map[string]interface{}{"role": "user", "content": content}
}
