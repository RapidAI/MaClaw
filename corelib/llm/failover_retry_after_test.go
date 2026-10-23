package llm

import (
	"testing"
	"time"
)

func TestMarkFailedFromErrorUsesRetryAfter(t *testing.T) {
	fc := NewFailoverChain([]FailoverProvider{{Name: "a", URL: "https://a", Key: "k", Model: "m"}, {Name: "b", URL: "https://b", Key: "k", Model: "m"}})
	err := &HTTPStatusError{StatusCode: 429, Body: []byte(`{"retry_after_seconds":90}`)}
	fc.MarkFailedFromError("a", err)
	if p := fc.NextAvailable(); p == nil || p.Name != "b" {
		t.Fatalf("want b, got %#v", p)
	}
	until := fc.cooldowns["a"]
	if d := time.Until(until); d < 80*time.Second {
		t.Fatalf("cooldown too short: %v", d)
	}
}
