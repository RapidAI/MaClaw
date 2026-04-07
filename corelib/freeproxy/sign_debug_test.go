package freeproxy

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSignDebug examines the WASM signing output in detail.
func TestSignDebug(t *testing.T) {
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

	// Sign multiple times to see if nonce/timestamp change
	for i := 0; i < 3; i++ {
		sr, err := signer.Sign(ctx, body, url)
		if err != nil {
			t.Fatalf("Sign[%d]: %v", i, err)
		}
		t.Logf("Sign[%d]: sign=%s nonce=%s ts=%d", i, sr.Sign, sr.Nonce, sr.Timestamp)
		t.Logf("  sign length: %d chars", len(sr.Sign))
		t.Logf("  nonce length: %d chars", len(sr.Nonce))
		t.Logf("  timestamp digits: %d", countDigits(sr.Timestamp))
	}

	// Compare: what does v1 signing look like?
	ts := int64(1774257743)
	nonce := "v3E6flXgaoh55MM2mlmFS"
	v1 := v1Sign(ts, body, nonce)
	t.Logf("v1Sign: %s (len=%d)", v1, len(v1))

	// Test: does the WASM timestamp look like seconds or milliseconds?
	sr, _ := signer.Sign(ctx, body, url)
	if sr.Timestamp > 1e12 {
		t.Logf("Timestamp is in MILLISECONDS: %d", sr.Timestamp)
	} else {
		t.Logf("Timestamp is in SECONDS: %d", sr.Timestamp)
	}
}

func countDigits(n int64) int {
	if n == 0 {
		return 1
	}
	count := 0
	v := n
	for v > 0 {
		count++
		v /= 10
	}
	return count
}

// TestSignCompareV1V2 compares v1 MD5 signing with WASM v2 signing.
func TestSignCompareV1V2(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	signer := NewWasmSigner(cacheDir)
	ctx := context.Background()
	defer signer.Close(ctx)

	if err := signer.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	body := `{"stream":true,"botCode":"AI_SEARCH","conversationId":"12345","question":"hi","agentId":""}`

	sr, err := signer.Sign(ctx, body, "/chatApi/v2/chat")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// v1 MD5 sign using the WASM's timestamp and nonce
	v1 := v1Sign(sr.Timestamp, body, sr.Nonce)

	t.Logf("WASM sign:  %s", sr.Sign)
	t.Logf("v1 MD5:     %s", v1)
	t.Logf("Same?       %v", sr.Sign == v1)

	// Try various MD5 combinations with URL
	combos := []struct {
		name string
		data string
	}{
		{"ts+body+nonce", fmt.Sprintf("%d%s%s", sr.Timestamp, body, sr.Nonce)},
		{"ts+body+nonce+url", fmt.Sprintf("%d%s%s%s", sr.Timestamp, body, sr.Nonce, "/chatApi/v2/chat")},
		{"ts+url+body+nonce", fmt.Sprintf("%d%s%s%s", sr.Timestamp, "/chatApi/v2/chat", body, sr.Nonce)},
		{"url+ts+body+nonce", fmt.Sprintf("%s%d%s%s", "/chatApi/v2/chat", sr.Timestamp, body, sr.Nonce)},
		{"tsMs+body+nonce", fmt.Sprintf("%d%s%s", sr.Timestamp*1000, body, sr.Nonce)},
		{"nonce+ts+body", fmt.Sprintf("%s%d%s", sr.Nonce, sr.Timestamp, body)},
		{"body+ts+nonce", fmt.Sprintf("%s%d%s", body, sr.Timestamp, sr.Nonce)},
	}

	for _, c := range combos {
		hash := md5.Sum([]byte(c.data))
		result := strings.ToUpper(fmt.Sprintf("%x", hash))
		match := ""
		if result == sr.Sign {
			match = " *** MATCH ***"
		}
		t.Logf("  %s: %s%s", c.name, result, match)
	}
}
