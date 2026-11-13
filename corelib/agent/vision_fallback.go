package agent

import (
	"encoding/base64"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// VisionSeesImages reports a cached image-input decision for the model that
// is about to be called. known is false when the caller should keep
// cfg.SupportsVision.
var VisionSeesImages func(cfg corelib.MaclawLLMConfig) (sees, known bool)

// NoteVisionRejected records that this model refused an image, so later
// steps in the turn use the OCR path.
var NoteVisionRejected func(cfg corelib.MaclawLLMConfig)

// VisionReading turns a rejected screenshot into text the model can use
// for the rest of this turn. Empty leaves the retry without that reading.
var VisionReading func(png []byte) string

func llmCallWithoutRejectedImages(cfg corelib.MaclawLLMConfig, conversation []interface{}, call func([]interface{}) (*llm.Response, error)) (*llm.Response, error) {
	resp, err := call(conversation)
	if !llm.ImageInputRejected(err) {
		return resp, err
	}
	next, changed := stripRejectedImages(conversation)
	if !changed {
		return resp, err
	}
	log.Printf("[agent-loop] image input rejected model=%s provider=%s; retrying this turn without images", cfg.Model, cfg.ProviderName)
	if NoteVisionRejected != nil {
		NoteVisionRejected(cfg)
	}
	return call(next)
}

// stripRejectedImages drops image blocks and appends one OCR reading when a
// host provided one. The bool is false when the conversation had no image.
func stripRejectedImages(conversation []interface{}) ([]interface{}, bool) {
	changed := false
	out := make([]interface{}, 0, len(conversation)+1)
	var readings []string
	for _, raw := range conversation {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			out = append(out, raw)
			continue
		}
		blocks, ok := msg["content"].([]interface{})
		if !ok {
			out = append(out, raw)
			continue
		}
		kept := make([]interface{}, 0, len(blocks))
		for _, block := range blocks {
			bm, ok := block.(map[string]interface{})
			if !ok {
				kept = append(kept, block)
				continue
			}
			kind, _ := bm["type"].(string)
			if kind != "image_url" && kind != "image" && kind != "input_image" {
				kept = append(kept, block)
				continue
			}
			changed = true
			if png := imagePNG(bm); len(png) > 0 && VisionReading != nil {
				if text := strings.TrimSpace(VisionReading(png)); text != "" {
					readings = append(readings, text)
				}
			}
		}
		if len(kept) == 0 {
			continue
		}
		if len(kept) == len(blocks) {
			out = append(out, raw)
			continue
		}
		clone := make(map[string]interface{}, len(msg))
		for k, v := range msg {
			clone[k] = v
		}
		clone["content"] = kept
		out = append(out, clone)
	}
	if len(readings) > 0 {
		out = append(out, map[string]interface{}{
			"role":    "user",
			"content": strings.Join(readings, "\n"),
		})
	}
	return out, changed
}

func imagePNG(block map[string]interface{}) []byte {
	if imageURL, ok := block["image_url"].(map[string]interface{}); ok {
		if raw, _ := imageURL["url"].(string); raw != "" {
			return decodeDataImage(raw)
		}
	}
	if source, ok := block["source"].(map[string]interface{}); ok {
		if data, _ := source["data"].(string); data != "" {
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err == nil {
				return decoded
			}
		}
	}
	return nil
}

func decodeDataImage(raw string) []byte {
	_, data, ok := strings.Cut(raw, "base64,")
	if !ok {
		data = raw
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil
	}
	return decoded
}

func modelSeesImages(cfg corelib.MaclawLLMConfig) bool {
	if VisionSeesImages == nil {
		return cfg.SupportsVision
	}
	sees, known := VisionSeesImages(cfg)
	if !known {
		return cfg.SupportsVision
	}
	return sees
}
