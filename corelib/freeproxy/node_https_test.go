package freeproxy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestNodeHTTPSSend tests browser-sim signing + Node.js native https send.
// This eliminates Go's TLS/HTTP stack entirely.
func TestNodeHTTPSSend(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(cacheDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	client := NewDangbeiClient(auth)
	ctx := context.Background()
	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	cookie := auth.GetCookie()

	wd, _ := os.Getwd()
	scriptPath := filepath.Join(wd, "node_https_send.mjs")
	cmd := exec.Command("node", scriptPath, cookie, convID)

	t.Log("Running Node.js https send with browser-sim signing...")
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	t.Logf("Completed in %v", elapsed)
	outStr := string(out)
	if len(outStr) > 3000 {
		outStr = outStr[:3000]
	}
	t.Logf("Output:\n%s", outStr)

	if err != nil {
		t.Logf("Exit error: %v", err)
	}

	// Check if we got a successful response
	if contains(outStr, "Status: 200") {
		t.Log("SUCCESS — browser-sim signing + Node.js https works!")
	} else if contains(outStr, "TIMEOUT") {
		t.Log("TIMEOUT — signing is still wrong or server requires something else")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && fmt.Sprintf("%s", s) != "" && len(substr) > 0 && findSubstring(s, substr)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
