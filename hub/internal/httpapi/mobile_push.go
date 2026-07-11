package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// Mobile push: device registration + offline pending queue + optional remote transport.
//
// Transports (any enables features.push_notifications):
//
//	MACLAW_MOBILE_PUSH_WEBHOOK_URL  — POST JSON per device delivery (ops/test)
//	MACLAW_MOBILE_FCM_SERVER_KEY    — legacy FCM HTTP API (Authorization: key=…)
//	MACLAW_MOBILE_PUSH_ENABLED=1    — log-only remote (dev; still registers capability)
//
// Pending sync works even without remote transport so cold-start apps can catch
// completions that happened while the realtime socket was down.

const (
	mobilePushPendingMaxPerUser = 50
	mobilePushPendingTTL        = 72 * time.Hour
	mobilePushDeviceMaxPerUser  = 8
)

type mobilePushDevice struct {
	Platform  string    `json:"platform"`
	Token     string    `json:"token"`
	DeviceID  string    `json:"device_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type mobilePushPendingItem struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Payload   string         `json:"payload,omitempty"`
	Status    string         `json:"status,omitempty"`
	TaskID    string         `json:"task_id,omitempty"`
	DedupeKey string         `json:"dedupe_key,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

var mobilePushState = struct {
	sync.Mutex
	devices  map[string][]mobilePushDevice // key: tenant|user
	pending  map[string][]mobilePushPendingItem
	lastPush map[string]time.Time // dedupe remote spam by dedupe_key
}{
	devices:  make(map[string][]mobilePushDevice),
	pending:  make(map[string][]mobilePushPendingItem),
	lastPush: make(map[string]time.Time),
}

func mobilePushUserKey(tenantID, userID string) string {
	return strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(userID)
}

func mobilePushWebhookURL() string {
	return strings.TrimSpace(os.Getenv("MACLAW_MOBILE_PUSH_WEBHOOK_URL"))
}

func mobilePushFCMServerKey() string {
	return strings.TrimSpace(os.Getenv("MACLAW_MOBILE_FCM_SERVER_KEY"))
}

func mobilePushLogOnlyEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("MACLAW_MOBILE_PUSH_ENABLED")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// mobilePushRemoteConfigured reports whether Hub may claim remote push capability.
func mobilePushRemoteConfigured() bool {
	return mobilePushWebhookURL() != "" || mobilePushFCMServerKey() != "" || mobilePushLogOnlyEnabled()
}

func mobilePushTransportSummary() map[string]any {
	return map[string]any{
		"remote_configured": mobilePushRemoteConfigured(),
		"webhook":           mobilePushWebhookURL() != "",
		"fcm_legacy":        mobilePushFCMServerKey() != "",
		"log_only":          mobilePushLogOnlyEnabled(),
		"pending_sync":      true,
		"persisted":         mobileStatePath() != "",
	}
}

// mobilePushLoadFromState hydrates in-memory maps from persistent mobile state.
func mobilePushLoadFromState(devices map[string][]mobilePushDevice, pending map[string][]mobilePushPendingItem) {
	mobilePushState.Lock()
	defer mobilePushState.Unlock()
	if devices != nil {
		mobilePushState.devices = make(map[string][]mobilePushDevice, len(devices))
		for k, list := range devices {
			cp := make([]mobilePushDevice, len(list))
			copy(cp, list)
			mobilePushState.devices[k] = cp
		}
	}
	if pending != nil {
		cutoff := time.Now().UTC().Add(-mobilePushPendingTTL)
		mobilePushState.pending = make(map[string][]mobilePushPendingItem, len(pending))
		for k, list := range pending {
			kept := make([]mobilePushPendingItem, 0, len(list))
			for _, p := range list {
				if p.CreatedAt.Before(cutoff) {
					continue
				}
				kept = append(kept, p)
			}
			if len(kept) > 0 {
				mobilePushState.pending[k] = kept
			}
		}
	}
}

// mobilePushSnapshotInto copies device/pending maps into the persistent state blob.
func mobilePushSnapshotInto(state *mobilePersistentState) {
	if state == nil {
		return
	}
	mobilePushState.Lock()
	defer mobilePushState.Unlock()
	if state.PushDevices == nil {
		state.PushDevices = make(map[string][]mobilePushDevice)
	}
	if state.PushPending == nil {
		state.PushPending = make(map[string][]mobilePushPendingItem)
	}
	for k, list := range mobilePushState.devices {
		cp := make([]mobilePushDevice, len(list))
		copy(cp, list)
		state.PushDevices[k] = cp
	}
	for k, list := range mobilePushState.pending {
		cp := make([]mobilePushPendingItem, len(list))
		copy(cp, list)
		state.PushPending[k] = cp
	}
}

func mobilePushResetForTest() {
	mobilePushState.Lock()
	mobilePushState.devices = make(map[string][]mobilePushDevice)
	mobilePushState.pending = make(map[string][]mobilePushPendingItem)
	mobilePushState.lastPush = make(map[string]time.Time)
	mobilePushState.Unlock()
}

func mobilePushSchedulePersist() {
	// Best-effort: reuse the shared mobile state file (documents/employees/push).
	go mobilePersistState()
}

func mobileNewPushID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("mpush_%d", time.Now().UnixNano())
	}
	return "mpush_" + hex.EncodeToString(b[:])
}

func mobileNormalizePushPlatform(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "fcm", "android":
		return "fcm"
	case "apns", "ios":
		return "apns"
	case "hms", "huawei":
		return "hms"
	case "device", "local", "webhook":
		return "device"
	default:
		return ""
	}
}

func mobilePushUpsertDevice(tenantID, userID string, dev mobilePushDevice) {
	key := mobilePushUserKey(tenantID, userID)
	dev.Platform = mobileNormalizePushPlatform(dev.Platform)
	dev.Token = strings.TrimSpace(dev.Token)
	dev.DeviceID = strings.TrimSpace(dev.DeviceID)
	if dev.Platform == "" || dev.Token == "" {
		return
	}
	dev.UpdatedAt = time.Now().UTC()
	mobilePushState.Lock()
	list := mobilePushState.devices[key]
	out := make([]mobilePushDevice, 0, len(list)+1)
	for _, d := range list {
		if d.Token == dev.Token || (dev.DeviceID != "" && d.DeviceID == dev.DeviceID) {
			continue
		}
		out = append(out, d)
	}
	out = append(out, dev)
	if len(out) > mobilePushDeviceMaxPerUser {
		out = out[len(out)-mobilePushDeviceMaxPerUser:]
	}
	mobilePushState.devices[key] = out
	mobilePushState.Unlock()
	mobilePushSchedulePersist()
}

func mobilePushRemoveDevice(tenantID, userID, token string) bool {
	key := mobilePushUserKey(tenantID, userID)
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	mobilePushState.Lock()
	list := mobilePushState.devices[key]
	if len(list) == 0 {
		mobilePushState.Unlock()
		return false
	}
	out := list[:0]
	removed := false
	for _, d := range list {
		if d.Token == token {
			removed = true
			continue
		}
		out = append(out, d)
	}
	if removed {
		if len(out) == 0 {
			delete(mobilePushState.devices, key)
		} else {
			mobilePushState.devices[key] = out
		}
	}
	mobilePushState.Unlock()
	if removed {
		mobilePushSchedulePersist()
	}
	return removed
}

func mobilePushListDevices(tenantID, userID string) []mobilePushDevice {
	key := mobilePushUserKey(tenantID, userID)
	mobilePushState.Lock()
	defer mobilePushState.Unlock()
	src := mobilePushState.devices[key]
	out := make([]mobilePushDevice, len(src))
	copy(out, src)
	return out
}

func mobilePushEnqueue(tenantID, userID string, item mobilePushPendingItem) mobilePushPendingItem {
	key := mobilePushUserKey(tenantID, userID)
	if item.ID == "" {
		item.ID = mobileNewPushID()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	mobilePushState.Lock()
	list := mobilePushState.pending[key]
	// Replace same dedupe_key (status transitions).
	if item.DedupeKey != "" {
		filtered := list[:0]
		for _, p := range list {
			if p.DedupeKey == item.DedupeKey {
				continue
			}
			filtered = append(filtered, p)
		}
		list = filtered
	}
	list = append(list, item)
	// Drop expired + cap.
	cutoff := time.Now().UTC().Add(-mobilePushPendingTTL)
	kept := list[:0]
	for _, p := range list {
		if p.CreatedAt.Before(cutoff) {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) > mobilePushPendingMaxPerUser {
		kept = kept[len(kept)-mobilePushPendingMaxPerUser:]
	}
	mobilePushState.pending[key] = kept
	mobilePushState.Unlock()
	mobilePushSchedulePersist()
	return item
}

func mobilePushListPending(tenantID, userID string) []mobilePushPendingItem {
	key := mobilePushUserKey(tenantID, userID)
	mobilePushState.Lock()
	defer mobilePushState.Unlock()
	cutoff := time.Now().UTC().Add(-mobilePushPendingTTL)
	src := mobilePushState.pending[key]
	kept := src[:0]
	out := make([]mobilePushPendingItem, 0, len(src))
	for _, p := range src {
		if p.CreatedAt.Before(cutoff) {
			continue
		}
		kept = append(kept, p)
		out = append(out, p)
	}
	if len(kept) == 0 {
		delete(mobilePushState.pending, key)
	} else {
		mobilePushState.pending[key] = kept
	}
	return out
}

func mobilePushAck(tenantID, userID string, ids []string) int {
	key := mobilePushUserKey(tenantID, userID)
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = struct{}{}
		}
	}
	if len(want) == 0 {
		return 0
	}
	mobilePushState.Lock()
	list := mobilePushState.pending[key]
	kept := list[:0]
	acked := 0
	for _, p := range list {
		if _, ok := want[p.ID]; ok {
			acked++
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		delete(mobilePushState.pending, key)
	} else {
		mobilePushState.pending[key] = kept
	}
	mobilePushState.Unlock()
	if acked > 0 {
		mobilePushSchedulePersist()
	}
	return acked
}

func mobileIsTerminalStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" {
		return false
	}
	// Avoid progress noise.
	if strings.Contains(s, "run") || strings.Contains(s, "progress") ||
		strings.Contains(s, "pending") || strings.Contains(s, "stream") ||
		strings.Contains(s, "queued") || strings.Contains(s, "claim") {
		// completed/cancelled may also contain substrings? "cancelled" has no run.
		// "running" has run — good. "failed" ok.
		if !strings.Contains(s, "complete") && !strings.Contains(s, "fail") &&
			!strings.Contains(s, "error") && !strings.Contains(s, "cancel") &&
			!strings.Contains(s, "ready") && !strings.Contains(s, "success") {
			return false
		}
	}
	return strings.Contains(s, "complete") ||
		strings.Contains(s, "ready") ||
		strings.Contains(s, "success") ||
		strings.Contains(s, "fail") ||
		strings.Contains(s, "error") ||
		strings.Contains(s, "cancel") ||
		strings.Contains(s, "done")
}

func mobileExtractEventStatus(event map[string]any) string {
	if event == nil {
		return ""
	}
	if s, _ := event["status"].(string); strings.TrimSpace(s) != "" {
		return s
	}
	for _, nest := range []string{"task", "operation", "session", "job", "payload"} {
		if m, ok := event[nest].(map[string]any); ok {
			if s, _ := m["status"].(string); strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func mobileExtractEventTaskID(event map[string]any) string {
	if event == nil {
		return ""
	}
	for _, k := range []string{"task_id", "job_id", "operation_id", "session_id"} {
		if s, _ := event[k].(string); strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	for _, nest := range []string{"task", "operation", "session", "job", "payload"} {
		m, ok := event[nest].(map[string]any)
		if !ok {
			continue
		}
		for _, k := range []string{"task_id", "job_id", "operation_id", "session_id"} {
			if s, _ := m[k].(string); strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func mobilePushTitleBody(event map[string]any) (title, body, payload string) {
	typ, _ := event["type"].(string)
	typ = strings.ToLower(strings.TrimSpace(typ))
	status := mobileExtractEventStatus(event)
	taskID := mobileExtractEventTaskID(event)
	failed := strings.Contains(strings.ToLower(status), "fail") ||
		strings.Contains(strings.ToLower(status), "error")
	cancelled := strings.Contains(strings.ToLower(status), "cancel")

	switch typ {
	case "ssh_task":
		title = "SSH 任务完成"
		if failed {
			title = "SSH 任务失败"
		} else if cancelled {
			title = "SSH 任务已取消"
		}
		payload = "ssh-task:" + taskID
		if task, ok := event["task"].(map[string]any); ok {
			cmd, _ := task["command"].(string)
			msg, _ := task["message"].(string)
			body = strings.TrimSpace(strings.Join(mobilePushNonEmpty(mobileClipRunes(cmd, 48), msg), " · "))
		}
	case "assistant_job", "agent_job":
		title = "助手任务完成"
		if failed {
			title = "助手任务失败"
		}
		payload = "assistant-task:" + taskID
		if task, ok := event["task"].(map[string]any); ok {
			q, _ := task["query"].(string)
			if q == "" {
				q, _ = task["title"].(string)
			}
			msg, _ := task["message"].(string)
			body = strings.TrimSpace(strings.Join(mobilePushNonEmpty(mobileClipRunes(q, 48), msg), " · "))
		}
	case "ssh_file_operation":
		title = "SSH 文件操作完成"
		if failed {
			title = "SSH 文件操作失败"
		}
		payload = "ssh-file:" + taskID
		if op, ok := event["operation"].(map[string]any); ok {
			action, _ := op["action"].(string)
			path, _ := op["remote_path"].(string)
			if path == "" {
				path, _ = op["local_path"].(string)
			}
			msg, _ := op["message"].(string)
			body = strings.TrimSpace(strings.Join(mobilePushNonEmpty(action, path, msg), " · "))
		}
	case "document_task":
		title = "文档任务完成"
		if failed {
			title = "文档任务失败"
		}
		if strings.Contains(strings.ToLower(taskID), "export") || statusLooksExport(event) {
			payload = "document-export:" + taskID
		} else {
			payload = "document-upload:" + taskID
		}
		if task, ok := event["task"].(map[string]any); ok {
			msg, _ := task["message"].(string)
			body = msg
		}
	case "digital_employee_task":
		title = "数字员工任务完成"
		if failed {
			title = "数字员工任务失败"
		}
		payload = "digital-employee-task:" + taskID
		if task, ok := event["task"].(map[string]any); ok {
			msg, _ := task["message"].(string)
			if msg == "" {
				msg, _ = task["result"].(string)
			}
			body = mobileClipRunes(msg, 120)
		}
	default:
		title = "MaClaw 任务更新"
		body = status
		payload = typ + ":" + taskID
	}
	if body == "" {
		body = status
	}
	if body == "" {
		body = taskID
	}
	return title, body, payload
}

func statusLooksExport(event map[string]any) bool {
	raw, _ := json.Marshal(event)
	return strings.Contains(strings.ToLower(string(raw)), "export")
}

func mobilePushNonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// mobilePushOnRealtimeEvent enqueues pending + optionally delivers remote push
// when the viewer has no live realtime socket (or remote is always preferred for terminal).
func mobilePushOnRealtimeEvent(tenantID, userID string, event map[string]any, realtimeClients int) {
	if event == nil || strings.TrimSpace(userID) == "" {
		return
	}
	typ, _ := event["type"].(string)
	typ = strings.ToLower(strings.TrimSpace(typ))
	switch typ {
	case "ssh_task", "ssh_file_operation", "document_task", "digital_employee_task",
		"assistant_job", "agent_job":
	default:
		return
	}
	status := mobileExtractEventStatus(event)
	if !mobileIsTerminalStatus(status) {
		return
	}
	taskID := mobileExtractEventTaskID(event)
	title, body, payload := mobilePushTitleBody(event)
	dedupe := typ + ":" + taskID + ":" + strings.ToLower(strings.TrimSpace(status))
	item := mobilePushEnqueue(tenantID, userID, mobilePushPendingItem{
		Type:      typ,
		Title:     title,
		Body:      body,
		Payload:   payload,
		Status:    status,
		TaskID:    taskID,
		DedupeKey: dedupe,
		Data: map[string]any{
			"type":   typ,
			"status": status,
			"task_id": taskID,
		},
	})

	// When app is online via WS it already shows local notifications; still keep
	// pending for multi-device. Remote only when no live clients (battery/noise).
	if realtimeClients > 0 && !mobilePushLogOnlyEnabled() {
		// still allow remote if only webhook/fcm and env forces always
		if strings.TrimSpace(os.Getenv("MACLAW_MOBILE_PUSH_ALWAYS")) == "" {
			return
		}
	}
	mobilePushDeliverRemote(tenantID, userID, item)
}

func mobilePushDeliverRemote(tenantID, userID string, item mobilePushPendingItem) {
	if !mobilePushRemoteConfigured() {
		return
	}
	// Throttle identical remote pushes (5s).
	mobilePushState.Lock()
	if item.DedupeKey != "" {
		if t, ok := mobilePushState.lastPush[item.DedupeKey]; ok && time.Since(t) < 5*time.Second {
			mobilePushState.Unlock()
			return
		}
		mobilePushState.lastPush[item.DedupeKey] = time.Now()
	}
	devices := append([]mobilePushDevice(nil), mobilePushState.devices[mobilePushUserKey(tenantID, userID)]...)
	mobilePushState.Unlock()

	if len(devices) == 0 {
		// Webhook without devices: still notify ops sink once.
		if url := mobilePushWebhookURL(); url != "" {
			_ = mobilePushPostWebhook(url, map[string]any{
				"tenant_id": tenantID,
				"user_id":   userID,
				"title":     item.Title,
				"body":      item.Body,
				"payload":   item.Payload,
				"data":      item.Data,
				"push_id":   item.ID,
				"no_device": true,
			})
		}
		if mobilePushLogOnlyEnabled() {
			log.Printf("[mobile-push] no devices tenant=%s user=%s title=%q", tenantID, userID, item.Title)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for _, d := range devices {
		switch d.Platform {
		case "fcm":
			if key := mobilePushFCMServerKey(); key != "" {
				if err := mobilePushSendFCMLegacy(ctx, key, d.Token, item); err != nil {
					log.Printf("[mobile-push] fcm error: %v", err)
				}
			}
		}
		if url := mobilePushWebhookURL(); url != "" {
			_ = mobilePushPostWebhook(url, map[string]any{
				"tenant_id":  tenantID,
				"user_id":    userID,
				"platform":   d.Platform,
				"token":      d.Token,
				"device_id":  d.DeviceID,
				"title":      item.Title,
				"body":       item.Body,
				"payload":    item.Payload,
				"data":       item.Data,
				"push_id":    item.ID,
			})
		}
		if mobilePushLogOnlyEnabled() {
			log.Printf("[mobile-push] deliver platform=%s user=%s title=%q body=%q", d.Platform, userID, item.Title, item.Body)
		}
	}
}

func mobilePushPostWebhook(url string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MaClaw-Hub-Mobile-Push/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[mobile-push] webhook error: %v", err)
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 300 {
		err = fmt.Errorf("webhook status %d", resp.StatusCode)
		log.Printf("[mobile-push] %v", err)
		return err
	}
	return nil
}

// mobilePushSendFCMLegacy uses the legacy FCM HTTP API (server key).
// Prefer webhook or FCM HTTP v1 in production; this is an ops-friendly fallback.
func mobilePushSendFCMLegacy(ctx context.Context, serverKey, deviceToken string, item mobilePushPendingItem) error {
	body := map[string]any{
		"to": deviceToken,
		"notification": map[string]any{
			"title": item.Title,
			"body":  item.Body,
		},
		"data": map[string]any{
			"push_id": item.ID,
			"type":    item.Type,
			"payload": item.Payload,
			"status":  item.Status,
			"task_id": item.TaskID,
		},
		"priority": "high",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fcm.googleapis.com/fcm/send", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "key="+serverKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("fcm status %d", resp.StatusCode)
	}
	return nil
}

// --- HTTP handlers ---

// MobilePushDevicesHandler registers or lists push devices for the viewer.
//
//	POST /api/mobile/push/devices  {platform, token, device_id?}
//	GET  /api/mobile/push/devices
//	DELETE /api/mobile/push/devices  {token}
func MobilePushDevicesHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		switch r.Method {
		case http.MethodGet:
			devs := mobilePushListDevices(principal.TenantID, principal.UserID)
			writeJSON(w, http.StatusOK, map[string]any{
				"devices":   devs,
				"transport": mobilePushTransportSummary(),
			})
		case http.MethodPost:
			var req struct {
				Platform string `json:"platform"`
				Token    string `json:"token"`
				DeviceID string `json:"device_id"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
				return
			}
			platform := mobileNormalizePushPlatform(req.Platform)
			token := strings.TrimSpace(req.Token)
			if platform == "" || token == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "platform and token are required")
				return
			}
			mobilePushUpsertDevice(principal.TenantID, principal.UserID, mobilePushDevice{
				Platform: platform,
				Token:    token,
				DeviceID: strings.TrimSpace(req.DeviceID),
			})
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":        true,
				"transport": mobilePushTransportSummary(),
			})
		case http.MethodDelete:
			var req struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req)
			token := strings.TrimSpace(req.Token)
			if token == "" {
				token = strings.TrimSpace(r.URL.Query().Get("token"))
			}
			if token == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "token is required")
				return
			}
			ok := mobilePushRemoveDevice(principal.TenantID, principal.UserID, token)
			writeJSON(w, http.StatusOK, map[string]any{"ok": ok})
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET, POST, or DELETE")
		}
	}
}

// MobilePushPendingHandler lists or acks offline completion notifications.
//
//	GET  /api/mobile/push/pending
//	POST /api/mobile/push/pending/ack  {ids:["…"]}
func MobilePushPendingHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		switch r.Method {
		case http.MethodGet:
			items := mobilePushListPending(principal.TenantID, principal.UserID)
			writeJSON(w, http.StatusOK, map[string]any{
				"items":       items,
				"count":       len(items),
				"transport":   mobilePushTransportSummary(),
				"server_time": time.Now().UTC().Format(time.RFC3339),
			})
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		}
	}
}

// MobilePushPendingAckHandler acknowledges pending items after local display.
func MobilePushPendingAckHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		n := mobilePushAck(principal.TenantID, principal.UserID, req.IDs)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "acked": n})
	}
}
