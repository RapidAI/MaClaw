package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestImageRejectionRetriesWithoutImagesAndRecordsTheDowngrade(t *testing.T) {
	png := "aGVsbG8=" // "hello"
	conversation := []interface{}{
		map[string]interface{}{"role": "user", "content": "look"},
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "[computer_observe screenshot]"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64," + png}},
		}},
	}
	previous := VisionReading
	noted := 0
	VisionReading = func(got []byte) string {
		if string(got) != "hello" {
			t.Fatalf("png = %q", got)
		}
		return "screen_vision:\n  ocr: 确定"
	}
	NoteVisionRejected = func(cfg corelib.MaclawLLMConfig) {
		noted++
		if cfg.Model != "see" {
			t.Fatalf("model = %s", cfg.Model)
		}
	}
	t.Cleanup(func() {
		VisionReading = previous
		NoteVisionRejected = nil
	})
	calls := 0
	resp, err := llmCallWithoutRejectedImages(corelib.MaclawLLMConfig{Model: "see", ProviderName: "eyes"}, conversation, func(conv []interface{}) (*llm.Response, error) {
		calls++
		if calls == 1 {
			return nil, &llm.HTTPStatusError{StatusCode: 400, Body: []byte(`{"error":"image input is unsupported"}`)}
		}
		raw, _ := conv[len(conv)-1].(map[string]interface{})
		text, _ := raw["content"].(string)
		if strings.Contains(text, "image_url") || !strings.Contains(text, "确定") {
			t.Fatalf("retry content = %#v", conv)
		}
		return &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Content: "ok"}}}}, nil
	})
	if err != nil || calls != 2 || noted != 1 || resp == nil || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("calls=%d noted=%d err=%v resp=%v", calls, noted, err, resp)
	}
}

func TestImageRejectionLeavesOtherErrorsAlone(t *testing.T) {
	calls := 0
	_, err := llmCallWithoutRejectedImages(corelib.MaclawLLMConfig{}, []interface{}{
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,YQ=="}},
		}},
	}, func([]interface{}) (*llm.Response, error) {
		calls++
		return nil, errors.New("timeout")
	})
	if calls != 1 || err == nil {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestModelSeesImagesUsesTheCachedDecision(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{SupportsVision: true, Model: "see"}
	if !modelSeesImages(cfg) {
		t.Fatal("without a cache the config flag stands")
	}
	previous := VisionSeesImages
	VisionSeesImages = func(corelib.MaclawLLMConfig) (bool, bool) { return false, true }
	t.Cleanup(func() { VisionSeesImages = previous })
	if modelSeesImages(cfg) {
		t.Fatal("a recorded downgrade must stop the next image")
	}
}
