package desktopd

import (
	"strconv"
	"sync"
	"time"
)

// viewHoldDefault / viewHoldMax bound how long one Hub view hold lasts
// without a refresh. The Hub refreshes while the page is open.
const (
	viewHoldDefault = 2 * time.Minute
	viewHoldMax     = 10 * time.Minute
)

// viewHoldTable records which bot displays the owner is watching right now.
// It lives only in memory: after a restart the next Hub poll restores it.
type viewHoldTable struct {
	mu    sync.Mutex
	until map[string]time.Time
	now   func() time.Time
}

var viewHolds = &viewHoldTable{until: map[string]time.Time{}, now: time.Now}

func (t *viewHoldTable) hold(key string, ttl any) map[string]any {
	d := viewHoldDefault
	switch v := ttl.(type) {
	case float64:
		d = time.Duration(v) * time.Second
	case int:
		d = time.Duration(v) * time.Second
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			d = time.Duration(n) * time.Second
		}
	}
	if d <= 0 {
		d = viewHoldDefault
	}
	if d > viewHoldMax {
		d = viewHoldMax
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	until := t.now().Add(d)
	t.until[key] = until
	return map[string]any{"ok": true, "viewer": true, "until": until.Unix()}
}

func (t *viewHoldTable) release(key string) map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.until, key)
	return map[string]any{"ok": true, "viewer": false}
}

func (t *viewHoldTable) active(key string) bool {
	if key == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	until, ok := t.until[key]
	if !ok {
		return false
	}
	if !t.now().Before(until) {
		delete(t.until, key)
		return false
	}
	return true
}
