package httpapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

const accountStreamQueueCap = 32

// accountStreamBroker is the phone SSE fanout. One subscription per account
// grant; a newer GET replaces the older one. The queue is 32 and non-blocking.
// Machine WebSocket Send is not used here, and a full machine buffer still
// closes that socket on the desktop path.
type accountStreamBroker struct {
	mu       sync.Mutex
	subs     map[string]*accountStreamSub
	cursors  map[string]map[string]uint64
	nextSub  uint64
}

type accountStreamSub struct {
	id      uint64
	session string
	queue     chan []byte
	gap       bool
	done      chan struct{}
	closeOnce sync.Once
	last      atomic.Uint64
}

func (s *accountStreamSub) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() { close(s.done) })
}

func newAccountStreamBroker() *accountStreamBroker {
	return &accountStreamBroker{
		subs:    map[string]*accountStreamSub{},
		cursors: map[string]map[string]uint64{},
	}
}

func (h remoteControlHandler) accountBroker() *accountStreamBroker {
	if h.streams != nil {
		return h.streams
	}
	return defaultAccountStreams
}

var defaultAccountStreams = newAccountStreamBroker()

func (h remoteControlHandler) enqueueAccountStream(grant remotecontrol.Grant, payload json.RawMessage) {
	if strings.TrimSpace(grant.ControllerMachineID) != "" {
		return
	}
	h.accountBroker().publish(grant.ID, payload)
}

func (b *accountStreamBroker) publish(grantID string, payload []byte) {
	if b == nil || grantID == "" || len(payload) == 0 {
		return
	}
	var head struct {
		SessionKey  string `json:"session_key"`
		ProjectPath string `json:"project_path"`
		Event       string `json:"event"`
		Cursor      uint64 `json:"cursor"`
		CursorFrom  uint64 `json:"cursor_from"`
	}
	_ = json.Unmarshal(payload, &head)
	key := canonicalRemoteSession(head.SessionKey, head.ProjectPath)
	b.mu.Lock()
	defer b.mu.Unlock()
	if head.Cursor > 0 && key != "" {
		b.setCursorLocked(grantID, key, head.Cursor)
	}
	sub := b.subs[grantID]
	if sub == nil || key == "" || key != sub.session {
		// Another task on the same grant must not move this stream's cursor.
		return
	}
	if head.Cursor > 0 {
		sub.last.Store(head.Cursor)
	}
	if sub.gap {
		snap := accountResyncFrame(grantID, head.SessionKey, head.ProjectPath, sub.last.Load())
		select {
		case sub.queue <- snap:
			sub.gap = false
		default:
			return
		}
	}
	select {
	case sub.queue <- append([]byte(nil), payload...):
	default:
		sub.gap = true
	}
}

func accountResyncFrame(grantID, sessionKey, projectPath string, cursor uint64) []byte {
	frame := map[string]any{
		"grant_id":    grantID,
		"event":       "snapshot",
		"resync":      true,
		"session_key": sessionKey,
		"project_path": projectPath,
	}
	if cursor > 0 {
		frame["cursor"] = cursor
	}
	raw, _ := json.Marshal(frame)
	return raw
}

func (b *accountStreamBroker) setCursorLocked(grantID, key string, cursor uint64) {
	set := b.cursors[grantID]
	if set == nil {
		set = map[string]uint64{}
		b.cursors[grantID] = set
	}
	if cursor > set[key] {
		set[key] = cursor
	}
}

func (b *accountStreamBroker) cursor(grantID, projectPath string) uint64 {
	key := canonicalRemoteSession("", projectPath)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cursors[grantID][key]
}

func (b *accountStreamBroker) subscribe(grantID, projectPath string, after uint64) *accountStreamSub {
	key := canonicalRemoteSession("", projectPath)
	b.mu.Lock()
	b.nextSub++
	sub := &accountStreamSub{
		id:      b.nextSub,
		session: key,
		queue:   make(chan []byte, accountStreamQueueCap),
		done:    make(chan struct{}),
	}
	if after > 0 {
		sub.last.Store(after)
		b.setCursorLocked(grantID, key, after)
	} else if cur := b.cursors[grantID][key]; cur > 0 {
		sub.last.Store(cur)
	}
	old := b.subs[grantID]
	b.subs[grantID] = sub
	b.mu.Unlock()
	if old != nil {
		old.close()
	}
	return sub
}

func (b *accountStreamBroker) unsubscribe(grantID string, sub *accountStreamSub) {
	if b == nil || sub == nil {
		return
	}
	b.mu.Lock()
	if b.subs[grantID] == sub {
		delete(b.subs, grantID)
	}
	b.mu.Unlock()
	sub.close()
}

func (h remoteControlHandler) accountStream(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.viewer(w, r)
	if !ok {
		return
	}
	grantID := strings.TrimSpace(r.PathValue("id"))
	grant, err := h.svc.ControllerGrant(r.Context(), principal.TenantID, principal.UserID, machineID, grantID)
	if err != nil {
		writeRemoteControlError(w, err)
		return
	}
	if strings.TrimSpace(grant.ControllerMachineID) != "" {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "桌面授权不使用这条流")
		return
	}
	projectPath := r.URL.Query().Get("project_path")
	if strings.TrimSpace(projectPath) == "" {
		writeError(w, http.StatusBadRequest, "INVALID", "缺少项目路径")
		return
	}
	after, _ := strconv.ParseUint(strings.TrimSpace(r.URL.Query().Get("after_cursor")), 10, 64)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "STREAM", "无法刷新事件流")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := h.accountBroker().subscribe(grant.ID, projectPath, after)
	defer h.accountBroker().unsubscribe(grant.ID, sub)
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	bw := bufio.NewWriter(w)
	write := func(block string) bool {
		if _, err := bw.WriteString(block); err != nil {
			return false
		}
		if err := bw.Flush(); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.done:
			return
		case <-tick.C:
			if !write(fmt.Sprintf(": cursor=%d\n\n", sub.last.Load())) {
				return
			}
		case payload := <-sub.queue:
			if !write("data: " + string(payload) + "\n\n") {
				return
			}
		}
	}
}

// canonicalRemoteSession folds a controlled project path so Windows and Linux
// hubs share one cursor key. D:\work\demo and d:/work/demo match.
func canonicalRemoteSession(sessionKey, projectPath string) string {
	raw := strings.TrimSpace(projectPath)
	if raw == "" {
		raw = strings.TrimSpace(sessionKey)
		const prefix = "desktop-user:"
		if strings.HasPrefix(raw, prefix) {
			raw = strings.TrimPrefix(raw, prefix)
		}
	}
	return canonicalRemotePath(raw)
}

func canonicalRemotePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "/", `\`)
	vol := ""
	rest := p
	if len(p) >= 2 && p[1] == ':' {
		vol = strings.ToUpper(p[:1]) + ":"
		rest = p[2:]
	}
	unc := strings.HasPrefix(p, `\`)
	parts := strings.Split(rest, `\`)
	stack := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack = append(stack, part)
		}
	}
	joined := strings.Join(stack, `\`)
	if vol != "" {
		if joined == "" {
			return vol + `\`
		}
		return vol + `\` + joined
	}
	if unc {
		return `\` + joined
	}
	return joined
}
