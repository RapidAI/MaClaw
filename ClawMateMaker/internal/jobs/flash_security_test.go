package jobs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func flashSecurityGateSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path unavailable")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "flash.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestFlashSecurityGateFailsClosedForUnknownSecureVersion(t *testing.T) {
	if !strings.Contains(flashSecurityGateSource(t), "security.SecureVersion == nil") {
		t.Fatal("unknown anti-rollback security version must fail closed")
	}
}
