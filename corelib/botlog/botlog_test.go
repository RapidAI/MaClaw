package botlog

import (
	"strings"
	"testing"
)

func TestRedactHidesSSHPassword(t *testing.T) {
	got := redact("", `{"ssh_password":"s3cret-not-in-result","status":"running"}`)
	if strings.Contains(got, "s3cret") || !strings.Contains(got, `"ssh_password":"[redacted]"`) {
		t.Fatal(got)
	}
}
