package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestEstimateLLMQuoteInputTokensOmitsPicturePayloads(t *testing.T) {
	text := map[string]any{
		"model": "official-mid",
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "建议几个标题"},
			},
		}},
	}
	picture := strings.Repeat("A", 8000)
	imageURL := "data:image/jpeg;base64," + picture
	withPicture := map[string]any{
		"model": "official-mid",
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "建议几个标题"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
				map[string]any{"type": "Image", "source": map[string]any{"type": "base64", "data": picture}},
				map[string]any{"type": "input_image", "image_url": imageURL},
			},
			"image_url": map[string]any{"url": imageURL},
		}},
	}
	if got, want := estimateLLMQuoteInputTokens(withPicture), estimateLLMQuoteInputTokens(text); got != want {
		t.Fatalf("picture prompt estimate = %d, text estimate = %d", got, want)
	}
	raw, err := json.Marshal(withPicture)
	if err != nil {
		t.Fatal(err)
	}
	if got, rawEstimate := estimateLLMQuoteInputTokens(withPicture), int64(corelib.EstimateTextTokens(string(raw))); got >= rawEstimate {
		t.Fatalf("stripped estimate %d is not below the data-URL body %d", got, rawEstimate)
	}
	kept := withPicture["messages"].([]any)[0].(map[string]any)["content"].([]any)[1].(map[string]any)["image_url"].(map[string]any)["url"]
	if kept != imageURL {
		t.Fatal("quote estimate rewrote the forwarded image")
	}

	plain := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "建议几个标题"}}}
	plainRaw, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := estimateLLMQuoteInputTokens(plain), int64(corelib.EstimateTextTokens(string(plainRaw))); got != want {
		t.Fatalf("text body estimate = %d, marshal estimate = %d", got, want)
	}
}
