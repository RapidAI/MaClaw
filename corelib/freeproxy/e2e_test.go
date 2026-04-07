package freeproxy

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestDiagnoseCookieDecryption reads the actual cookie DB from the maclaw
// browser profile and diagnoses decryption issues.
func TestDiagnoseCookieDecryption(t *testing.T) {
	requireIntegrationTest(t)
	requireWindows(t)

	profileDir := maclawUserDataDir()
	t.Logf("Profile dir: %s", profileDir)

	localStatePath := filepath.Join(profileDir, "Local State")
	if _, err := os.Stat(localStatePath); err != nil {
		t.Fatalf("Local State not found: %v", err)
	}
	t.Log("Local State found")

	key, err := getAESKey(profileDir)
	if err != nil {
		t.Fatalf("getAESKey failed: %v", err)
	}
	t.Logf("AES key length: %d bytes, hex: %s", len(key), hex.EncodeToString(key))

	cookieDBPath := findCookieDB()
	if cookieDBPath == "" {
		t.Fatal("Cookie DB not found")
	}
	t.Logf("Cookie DB: %s", cookieDBPath)

	tmpFile, err := os.CreateTemp("", "diag-cookies-*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command("cmd", "/c", "copy", "/y", cookieDBPath, tmpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("copy cookie db: %v (%s)", err, string(out))
	}

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		t.Fatalf("open cookie db: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT name, value, encrypted_value, host_key FROM cookies WHERE host_key LIKE ?`,
		"%dangbei.com",
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var name, value, hostKey string
		var encValue []byte
		if err := rows.Scan(&name, &value, &encValue, &hostKey); err != nil {
			t.Logf("scan error: %v", err)
			continue
		}
		count++
		t.Logf("Cookie: name=%q host=%q plaintext_value_len=%d encrypted_value_len=%d",
			name, hostKey, len(value), len(encValue))

		if value != "" {
			t.Logf("  -> plaintext value (first 50): %q", truncStr(value, 50))
			continue
		}

		if len(encValue) == 0 {
			t.Logf("  -> no value at all")
			continue
		}

		prefix := encValue
		if len(prefix) > 20 {
			prefix = prefix[:20]
		}
		t.Logf("  -> encrypted prefix hex: %s", hex.EncodeToString(prefix))
		if len(encValue) > 3 {
			t.Logf("  -> version prefix: %q", string(encValue[:3]))
		}

		dec, err := decryptCookieValue(encValue, profileDir)
		if err != nil {
			t.Logf("  -> DECRYPT FAILED: %v", err)
		} else {
			t.Logf("  -> decrypted len=%d, first 80: %q", len(dec), truncStr(dec, 80))
		}
	}
	t.Logf("Total dangbei cookies found: %d", count)
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestE2EDangbeiAPI reads the persisted cookie and makes a real API call
// to 当贝 AI to verify the full chain works.
func TestE2EDangbeiAPI(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(configDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	cookie := auth.GetCookie()
	if cookie == "" {
		t.Skip("No persisted cookie found, skipping E2E test")
	}
	t.Logf("Cookie length: %d", len(cookie))

	client := NewDangbeiClient(auth)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 1: Check authentication
	t.Log("Checking IsAuthenticated...")
	if !client.IsAuthenticated(ctx) {
		t.Fatal("IsAuthenticated returned false — cookie is invalid or expired")
	}
	t.Log("IsAuthenticated: true")

	// Step 2: Create conversation
	t.Log("Creating conversation...")
	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("Conversation ID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	// Step 3: Send completion
	t.Log("Sending completion (hello)...")
	cr := CompletionRequest{
		ConversationID: convID,
		Prompt:         "hello, reply in one short sentence",
	}
	fullText, _, err := client.StreamCompletion(ctx, cr, func(token string) {
		fmt.Print(token)
	})
	fmt.Println()
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	t.Logf("Full response length: %d chars", len(fullText))
	t.Logf("Response: %s", truncStr(fullText, 200))

	if len(fullText) == 0 {
		t.Error("Empty response — streaming may not be working correctly")
	}
}
