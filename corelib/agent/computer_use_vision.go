package agent

import (
	"os"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ComputerUseVisionImageMarker prefixes the user message that carries a
// computer_observe screenshot so later observes can drop stale images.
const ComputerUseVisionImageMarker = "[computer_observe screenshot]"

// DefaultComputerUseKeepImages is how many computer-use screenshots stay in
// the model context. Older ones are replaced by a short marker, the same
// idea as cua's ImageRetentionCallback (only_n_most_recent_images, MIT,
// https://github.com/trycua/cua). Override with
// MACLAW_COMPUTER_USE_KEEP_IMAGES (1 to 10).
const DefaultComputerUseKeepImages = 3

// ComputerUseKeepImages is the effective screenshot retention.
func ComputerUseKeepImages() int {
	raw := strings.TrimSpace(os.Getenv("MACLAW_COMPUTER_USE_KEEP_IMAGES"))
	if raw == "" {
		return DefaultComputerUseKeepImages
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return DefaultComputerUseKeepImages
	}
	if n > 10 {
		return 10
	}
	return n
}

func appendComputerUseVisionImages(conversation []interface{}, cfg corelib.MaclawLLMConfig, images []ToolModelImage) []interface{} {
	if len(images) == 0 || !cfg.SupportsVision {
		return conversation
	}
	conversation = pruneComputerUseVisionImages(conversation)
	return append(conversation, buildComputerUseVisionMessage(cfg.Protocol, images))
}

// pruneComputerUseVisionImages makes room for one more screenshot: it keeps
// the newest ComputerUseKeepImages()-1 images.
func pruneComputerUseVisionImages(conversation []interface{}) []interface{} {
	return keepNewestComputerUseVisionImages(conversation, ComputerUseKeepImages()-1)
}

// pruneOlderComputerUseVisionImages keeps the newest ComputerUseKeepImages()
// screenshots and omits the rest.
func pruneOlderComputerUseVisionImages(conversation []interface{}) []interface{} {
	return keepNewestComputerUseVisionImages(conversation, ComputerUseKeepImages())
}

func keepNewestComputerUseVisionImages(conversation []interface{}, keep int) []interface{} {
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
		if !ok || !computerUseVisionMessage(blocks) {
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
	return replaceComputerUseVisionImages(conversation, kept)
}

func replaceComputerUseVisionImages(conversation []interface{}, kept map[int]bool) []interface{} {
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
		if kept[i] {
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
