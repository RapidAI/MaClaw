package freeproxy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFetchDebug(t *testing.T) {
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
	cmd := exec.Command("node", filepath.Join(wd, "fetch_debug.mjs"), cookie, convID)
	out, err := cmd.CombinedOutput()
	outStr := string(out)
	if len(outStr) > 4000 {
		outStr = outStr[:4000]
	}
	t.Logf("Output:\n%s", outStr)
	if err != nil {
		t.Logf("Exit: %v", err)
	}
}
