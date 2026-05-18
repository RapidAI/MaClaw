package needledata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	Dir     string
	Enabled bool
	mu      sync.Mutex
}

func NewLogger(dir string, enabled bool) *Logger {
	return &Logger{Dir: dir, Enabled: enabled}
}

func (l *Logger) Log(e Event) error {
	if l == nil || !l.Enabled {
		return nil
	}
	if l.Dir == "" {
		return fmt.Errorf("needledata: empty log dir")
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	e = RedactEvent(e)
	if e.EventID == "" {
		e.EventID = fmt.Sprintf("%d", e.Timestamp.UnixNano())
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return err
	}
	name := e.Timestamp.Format("2006-01-02") + ".jsonl"
	f, err := os.OpenFile(filepath.Join(l.Dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

func DefaultLogDir(dataDir string) string {
	return filepath.Join(dataDir, "needle", "events")
}

func DefaultModelDir(dataDir string) string {
	return filepath.Join(dataDir, "needle", "models", "active")
}
