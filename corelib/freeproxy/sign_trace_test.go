package freeproxy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSignTrace runs get_sign with full debug to trace the crypto path.
// The key question: are random bytes correctly written back to WASM memory?
func TestSignTrace(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	signer := NewWasmSigner(cacheDir)
	signer.debug = true
	ctx := context.Background()
	defer signer.Close(ctx)

	if err := signer.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	body := `{"stream":true,"botCode":"AI_SEARCH","conversationId":"12345","question":"hi","agentId":""}`
	url := "/chatApi/v2/chat"

	sr, err := signer.Sign(ctx, body, url)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	t.Logf("Result: sign=%s nonce=%s ts=%d", sr.Sign, sr.Nonce, sr.Timestamp)
}
