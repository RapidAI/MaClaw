package weixin

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func TestWxLogUsesEffectiveMaclawLogsDir(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	base := t.TempDir()
	maclawpath.SetBaseDir(base)
	t.Cleanup(func() {
		if globalWxLog != nil {
			globalWxLog.Close()
		}
		globalWxLog = nil
		globalWxLogOnce = sync.Once{}
		maclawpath.SetBaseDir(oldBase)
	})

	globalWxLog = nil
	globalWxLogOnce = sync.Once{}
	logger := GetWxLog()
	if logger == nil {
		t.Fatal("GetWxLog() returned nil")
	}
	logger.Log("test", "out", "uid", "hello")
	logger.Close()

	path := filepath.Join(base, "logs", wxLogFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("wx log should be created under effective logs dir %q: %v", path, err)
	}
}
