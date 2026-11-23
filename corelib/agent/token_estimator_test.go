package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestEstimateBytesToTokensUsesCanonicalTextEstimator(t *testing.T) {
	for _, text := range []string{
		strings.Repeat("ascii payload ", 400),
		strings.Repeat("中文上下文", 400),
		strings.Repeat("mixed 中文 payload ", 300),
	} {
		data, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		want := corelib.EstimateTextTokens(string(data))
		if got := EstimateBytesToTokens(data); got != want {
			t.Fatalf("EstimateBytesToTokens() = %d, want canonical estimate %d", got, want)
		}
	}
}

func TestEstimateMessageTokensMatchesCanonicalContentCost(t *testing.T) {
	content := strings.Repeat("a", 4000)
	message := map[string]string{"role": "user", "content": content}
	got := EstimateMessageTokens(message)
	want := corelib.EstimateTextTokens("user") + corelib.EstimateTextTokens(content) + 4
	if got != want {
		t.Fatalf("EstimateMessageTokens() = %d, want %d", got, want)
	}
}
