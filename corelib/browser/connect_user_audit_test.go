package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditLoggerDoesNotPersistSensitiveNavigationOrActionDetails(t *testing.T) {
	logDir := t.TempDir()
	logger := NewAuditLogger(logDir)
	logger.LogNavigation("session-1", "https://user:pass@example.com/path?token=SECRET_TOKEN#SECRET_FRAGMENT")
	logger.LogAction("session-1", "click", "Browser: SECRET_DETAIL")
	logger.LogDisconnect("session-1", "Tool: SECRET_REASON")
	logger.Close()

	data, err := os.ReadFile(filepath.Join(logDir, "browser_connect_audit.log"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	logText := string(data)
	for _, leaked := range []string{"SECRET_TOKEN", "SECRET_FRAGMENT", "SECRET_DETAIL", "SECRET_REASON", "user:pass", "Browser:", "Tool:"} {
		if strings.Contains(logText, leaked) {
			t.Fatalf("audit log leaked %q: %s", leaked, logText)
		}
	}
	for _, expected := range []string{"url=https://example.com/path", "detail_len=22", "reason_len=19"} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("audit log missing %q: %s", expected, logText)
		}
	}
}
