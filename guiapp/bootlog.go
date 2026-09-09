package guiapp

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	bootLogMu   sync.Mutex
	bootLogPath string
)

func bootLog(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[boot] err=none %s", msg)
	bootLogMu.Lock()
	defer bootLogMu.Unlock()
	if bootLogPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dir := filepath.Join(home, ".maclaw", "logs")
		_ = os.MkdirAll(dir, 0o755)
		bootLogPath = filepath.Join(dir, "webview-recover.log")
	}
	f, err := os.OpenFile(bootLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
	_ = f.Close()
}
