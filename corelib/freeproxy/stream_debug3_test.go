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

// TestStreamDebugViaCurl uses curl.exe directly to bypass Go's HTTP stack entirely.
// This tells us if the issue is Go-specific or server-side.
func TestStreamDebugViaCurl(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(configDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	client := NewDangbeiClient(auth)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

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
	bodyStr := fmt.Sprintf(`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`, convID)

	// Write body to a temp file to avoid shell escaping issues
	tmpBody, _ := os.CreateTemp("", "curl-body-*.json")
	tmpBody.WriteString(bodyStr)
	tmpBody.Close()
	defer os.Remove(tmpBody.Name())

	t.Logf("Body: %s", bodyStr)
	t.Logf("Cookie: %s", cookie)

	cmd := exec.Command("curl.exe",
		"-v",
		"--max-time", "15",
		"-X", "POST",
		dangbeiAPIBase+"/chatApi/v2/chat",
		"-H", "Content-Type: application/json",
		"-H", "Origin: https://ai.dangbei.com",
		"-H", "Referer: https://ai.dangbei.com/",
		"-H", "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"-H", "Cookie: "+cookie,
		"-d", "@"+tmpBody.Name(),
	)

	t.Log("Running curl.exe...")
	out, err := cmd.CombinedOutput()
	t.Logf("curl output:\n%s", string(out))
	if err != nil {
		t.Logf("curl error: %v", err)
	}
}

// TestStreamDebugNonStream tries stream=false to see if the server responds at all.
func TestStreamDebugNonStream(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(configDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	client := NewDangbeiClient(auth)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

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
	bodyStr := fmt.Sprintf(`{"stream":false,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`, convID)

	tmpBody, _ := os.CreateTemp("", "curl-body-*.json")
	tmpBody.WriteString(bodyStr)
	tmpBody.Close()
	defer os.Remove(tmpBody.Name())

	t.Logf("Body (non-stream): %s", bodyStr)

	cmd := exec.Command("curl.exe",
		"-v",
		"--max-time", "15",
		"-X", "POST",
		dangbeiAPIBase+"/chatApi/v2/chat",
		"-H", "Content-Type: application/json",
		"-H", "Origin: https://ai.dangbei.com",
		"-H", "Referer: https://ai.dangbei.com/",
		"-H", "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"-H", "Cookie: "+cookie,
		"-d", "@"+tmpBody.Name(),
	)

	t.Log("Running curl.exe (non-stream)...")
	out, err := cmd.CombinedOutput()
	t.Logf("curl output:\n%s", string(out))
	if err != nil {
		t.Logf("curl error: %v", err)
	}
}
