package repomaint

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	hubstore "github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

const hookSecret = "repo-maint-hmac-secret-value"

type hookIdentity struct{}

func (hookIdentity) AuthenticateMachine(_ context.Context, machineID, _ string) (*auth.MachinePrincipal, error) {
	return &auth.MachinePrincipal{TenantID: hubstore.DefaultTenantID, UserID: "user-1", MachineID: machineID}, nil
}
func (hookIdentity) AuthenticateViewer(context.Context, string) (*auth.ViewerPrincipal, error) {
	return &auth.ViewerPrincipal{TenantID: hubstore.DefaultTenantID, UserID: "user-1"}, nil
}
func (hookIdentity) IssueViewerTokenForUser(context.Context, string) (string, error) {
	return "viewer-token", nil
}

type harness struct {
	t       *testing.T
	svc     *Service
	db      *sql.DB
	devices *device.Service
	url     string
	machine string
	ruleID  string
	token   string
}

func newHarness(t *testing.T, machine string, kinds []string, enabled bool) *harness {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	devices := device.NewService(nil, device.NewRuntime())
	svc, err := Open(db, devices)
	if err != nil {
		t.Fatal(err)
	}
	devices.SetOnlineHook(svc.PushPending)
	mux := http.NewServeMux()
	svc.Register(mux, hookIdentity{})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	h := &harness{
		t: t, svc: svc, db: db, devices: devices, url: server.URL,
		machine: machine, ruleID: "rule-" + machine, token: "hook-" + machine,
	}
	h.putRule(kinds, enabled, 0)
	return h
}

func (h *harness) putRule(kinds []string, enabled bool, hookID int64) {
	h.t.Helper()
	body, _ := json.Marshal(map[string]any{
		"machine_id":     h.machine,
		"owner":          "yucejade",
		"repo":           "jjquant-v2",
		"hook_token":     h.token,
		"hook_secret":    hookSecret,
		"github_hook_id": hookID,
		"enabled":        enabled,
		"push_branch":    "",
		"event_kinds":    kinds,
	})
	req, err := http.NewRequest(http.MethodPut, h.url+"/api/github/repo-maint/rules/"+h.ruleID, bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer machine-token")
	req.Header.Set("X-Machine-ID", h.machine)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("put rule status=%d body=%s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), hookSecret) {
		h.t.Fatalf("put rule returned the webhook secret: %s", raw)
	}
	var view rulePublic
	if err := json.Unmarshal(raw, &view); err != nil {
		h.t.Fatal(err)
	}
	if !view.SecretSet || view.RuleID != h.ruleID {
		h.t.Fatalf("put rule view=%+v", view)
	}
}

func (h *harness) post(event, delivery string, body []byte, sig string, extra http.Header) int {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.url+"/api/github/repo-maint/hooks/"+h.token, bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("X-GitHub-Event", event)
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	for key, values := range extra {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	h.t.Logf("POST event=%s delivery=%s status=%d rows=%d pending_queue=%d", event, delivery, resp.StatusCode, h.count(), device.PendingMessages.PendingCount(h.machine))
	return resp.StatusCode
}

func (h *harness) count() int {
	h.t.Helper()
	var n int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM github_repo_maint_deliveries`).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func (h *harness) statusByObject(objectID string) string {
	h.t.Helper()
	var status string
	err := h.db.QueryRow(`SELECT status FROM github_repo_maint_deliveries WHERE object_id=?`, objectID).Scan(&status)
	if err != nil {
		h.t.Fatalf("status %s: %v", objectID, err)
	}
	return status
}

func (h *harness) hookID() int64 {
	h.t.Helper()
	var id int64
	if err := h.db.QueryRow(`SELECT github_hook_id FROM github_repo_maint_rules WHERE rule_id=?`, h.ruleID).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) dial() *websocket.Conn {
	h.t.Helper()
	gateway := ws.NewGateway(hookIdentity{}, h.devices, session.NewService(session.NewCache(), nil))
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	h.t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.machine",
		"payload": map[string]any{"machine_id": h.machine, "machine_token": "token"},
	}); err != nil {
		h.t.Fatal(err)
	}
	var env struct {
		Type string `json:"type"`
	}
	if err := conn.ReadJSON(&env); err != nil || env.Type != "auth.ok" {
		h.t.Fatalf("auth response type=%s err=%v", env.Type, err)
	}
	return conn
}

func sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(hookSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func issueOpened(n int) []byte {
	id := strconv.Itoa(n)
	return []byte(`{"action":"opened","issue":{"number":` + id + `,"title":"bug","body":"hello","user":{"login":"octocat"},"html_url":"https://github.com/yucejade/jjquant-v2/issues/` + id + `","url":"https://api.github.com/repos/yucejade/jjquant-v2/issues/` + id + `"},"repository":{"name":"jjquant-v2","default_branch":"main","owner":{"login":"yucejade"}}}`)
}

var allKinds = []string{
	kindIssuesOpened, kindIssuesClosed, kindIssuesLabeled, kindIssueComment,
	kindPullOpened, kindPullMerged, kindReviewRequested, kindReviewSubmitted,
	kindPush, kindCheckFailure,
}

func TestWebhookContract(t *testing.T) {
	t.Run("bad signature and short mac", func(t *testing.T) {
		h := newHarness(t, "rm-badsig", allKinds, true)
		body := issueOpened(7)
		before := device.PendingMessages.PendingCount(h.machine)
		if status := h.post("issues", "deliv-bad", body, "sha256="+strings.Repeat("ab", 32), nil); status != http.StatusUnauthorized {
			t.Fatalf("bad signature status=%d", status)
		}
		short := h.post("issues", "deliv-short", body, "sha256=abcd", nil)
		if short != http.StatusUnauthorized {
			t.Fatalf("short mac status=%d", short)
		}
		upper := "sha256=" + strings.ToUpper(strings.TrimPrefix(sign(body), "sha256="))
		if status := h.post("issues", "deliv-upper", body, upper, nil); status != http.StatusUnauthorized {
			t.Fatalf("uppercase mac status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("rejected signatures stored %d rows", h.count())
		}
		if device.PendingMessages.PendingCount(h.machine) != before {
			t.Fatalf("pending queue grew on rejected signature")
		}
	})

	t.Run("duplicate delivery", func(t *testing.T) {
		h := newHarness(t, "rm-dup", allKinds, true)
		body := issueOpened(8)
		sig := sign(body)
		if status := h.post("issues", "deliv-dup", body, sig, nil); status != http.StatusOK {
			t.Fatalf("first status=%d", status)
		}
		if status := h.post("issues", "deliv-dup", body, sig, nil); status != http.StatusOK {
			t.Fatalf("duplicate status=%d", status)
		}
		if h.count() != 1 {
			t.Fatalf("rows=%d", h.count())
		}
	})

	t.Run("timed out check is not stored", func(t *testing.T) {
		h := newHarness(t, "rm-timeout", allKinds, true)
		body := []byte(`{"action":"completed","check_run":{"id":9,"name":"ci","conclusion":"timed_out","head_sha":"abc","pull_requests":[]},"repository":{"name":"jjquant-v2","owner":{"login":"yucejade"}}}`)
		if status := h.post("check_run", "deliv-timeout", body, sign(body), nil); status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("timed_out stored %d rows", h.count())
		}
	})

	t.Run("failing check keeps unmerged pull requests", func(t *testing.T) {
		h := newHarness(t, "rm-check", allKinds, true)
		body := []byte(`{"action":"completed","check_run":{"id":11,"name":"ci","conclusion":"failure","head_sha":"sha1","pull_requests":[{"number":4,"merged_at":null},{"number":9,"merged_at":"2020-01-01T00:00:00Z"},{"number":2,"merged_at":null}]},"repository":{"name":"jjquant-v2","owner":{"login":"yucejade"}}}`)
		if status := h.post("check_run", "deliv-fail", body, sign(body), nil); status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		if h.count() != 1 || h.statusByObject("check_run:11") != statusPending {
			t.Fatalf("rows=%d", h.count())
		}
		items := h.list()
		if len(items) != 1 || items[0].ObjectID != "check_run:11" {
			t.Fatalf("listed=%+v", items)
		}
		got := items[0].Payload.PRNumbers
		if len(got) != 2 || got[0] != 2 || got[1] != 4 {
			t.Fatalf("pr numbers=%v", got)
		}
		if strings.Contains(h.listRaw(), hookSecret) {
			t.Fatal("delivery list contains the webhook secret")
		}
	})

	t.Run("ping records hook id and stores nothing", func(t *testing.T) {
		h := newHarness(t, "rm-ping", allKinds, true)
		body := []byte(`{"zen":"keep it logically awesome"}`)
		header := http.Header{"X-GitHub-Hook-ID": []string{"42"}}
		if status := h.post("ping", "deliv-ping", body, sign(body), header); status != http.StatusOK {
			t.Fatalf("ping status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("ping stored %d rows", h.count())
		}
		if h.hookID() != 42 {
			t.Fatalf("hook id=%d", h.hookID())
		}
		issue := issueOpened(3)
		wrong := http.Header{"X-GitHub-Hook-ID": []string{"7"}}
		if status := h.post("issues", "deliv-wrong-hook", issue, sign(issue), wrong); status != http.StatusUnauthorized {
			t.Fatalf("mismatched hook id status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("mismatched hook id stored %d rows", h.count())
		}
	})

	t.Run("disabled rule stores nothing", func(t *testing.T) {
		h := newHarness(t, "rm-off", allKinds, false)
		body := issueOpened(4)
		if status := h.post("issues", "deliv-off", body, sign(body), nil); status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("disabled rule stored %d rows", h.count())
		}
	})

	t.Run("push without default branch is stored", func(t *testing.T) {
		h := newHarness(t, "rm-push", allKinds, true)
		body := []byte(`{"ref":"refs/heads/feature","after":"abc123def","repository":{"name":"jjquant-v2","owner":{"login":"yucejade"}},"pusher":{"name":"octocat"}}`)
		if status := h.post("push", "deliv-push", body, sign(body), nil); status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		if h.count() != 1 || h.statusByObject("commit:abc123def") != statusPending {
			t.Fatalf("rows=%d", h.count())
		}
		items := h.list()
		if len(items) != 1 || items[0].Payload.DefaultBranch != "" || items[0].EventKind != kindPush {
			t.Fatalf("push payload=%+v", items)
		}
	})

	t.Run("push on another branch is dropped when default branch is known", func(t *testing.T) {
		h := newHarness(t, "rm-push-other", allKinds, true)
		body := []byte(`{"ref":"refs/heads/feature","after":"abc123def","repository":{"name":"jjquant-v2","default_branch":"main","owner":{"login":"yucejade"}}}`)
		if status := h.post("push", "deliv-push-other", body, sign(body), nil); status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("other branch stored %d rows", h.count())
		}
	})

	t.Run("unmerged pull request close is dropped", func(t *testing.T) {
		h := newHarness(t, "rm-pr-close", allKinds, true)
		body := []byte(`{"action":"closed","pull_request":{"number":3,"merged":false,"title":"x"},"repository":{"name":"jjquant-v2","owner":{"login":"yucejade"}}}`)
		if status := h.post("pull_request", "deliv-pr-close", body, sign(body), nil); status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("unmerged close stored %d rows", h.count())
		}
	})

	t.Run("offline stays pending and does not grow the short queue", func(t *testing.T) {
		h := newHarness(t, "rm-offline", allKinds, true)
		before := device.PendingMessages.PendingCount(h.machine)
		body := issueOpened(12)
		if status := h.post("issues", "deliv-offline", body, sign(body), nil); status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		if h.count() != 1 || h.statusByObject("issue:12") != statusPending {
			t.Fatalf("offline row missing, rows=%d", h.count())
		}
		if got := device.PendingMessages.PendingCount(h.machine); got != before {
			t.Fatalf("short queue before=%d after=%d", before, got)
		}
		conn := h.dial()
		if got := device.PendingMessages.PendingCount(h.machine); got != before {
			t.Fatalf("short queue grew after connect: %d", got)
		}
		if err := h.devices.MarkOnline(context.Background(), h.machine, ws.MachineHelloPayload{
			Name: "desk", Nickname: "RepoMaint", Platform: "windows",
		}); err != nil {
			t.Fatal(err)
		}
		payload := readFrame(t, conn, FrameType)
		var ev RepoMaintEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.ObjectID != "issue:12" || ev.EventKind != kindIssuesOpened || ev.RuleID != h.ruleID {
			t.Fatalf("online frame=%+v", ev)
		}
		if h.statusByObject("issue:12") != statusPending {
			t.Fatal("online send acked the row before the desktop committed")
		}
		if got := device.PendingMessages.PendingCount(h.machine); got != before {
			t.Fatalf("short queue grew after online send: before=%d after=%d", before, got)
		}
		t.Logf("offline row delivered after MarkOnline via %s without pending queue growth", FrameType)
	})

	t.Run("already online send does not enqueue", func(t *testing.T) {
		h := newHarness(t, "rm-online", allKinds, true)
		conn := h.dial()
		before := device.PendingMessages.PendingCount(h.machine)
		body := issueOpened(13)
		if status := h.post("issues", "deliv-online", body, sign(body), nil); status != http.StatusOK {
			t.Fatalf("status=%d", status)
		}
		payload := readFrame(t, conn, FrameType)
		var ev RepoMaintEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.ObjectID != "issue:13" {
			t.Fatalf("frame=%+v", ev)
		}
		if got := device.PendingMessages.PendingCount(h.machine); got != before {
			t.Fatalf("short queue before=%d after=%d", before, got)
		}
		if h.statusByObject("issue:13") != statusPending {
			t.Fatal("row was not left pending")
		}
	})

	t.Run("pending limit returns 503 and does not insert", func(t *testing.T) {
		h := newHarness(t, "rm-limit", allKinds, true)
		h.svc.SetPendingLimit(1)
		first := issueOpened(1)
		second := issueOpened(2)
		if status := h.post("issues", "deliv-limit-1", first, sign(first), nil); status != http.StatusOK {
			t.Fatalf("first status=%d", status)
		}
		if status := h.post("issues", "deliv-limit-2", second, sign(second), nil); status != http.StatusServiceUnavailable {
			t.Fatalf("second status=%d", status)
		}
		if h.count() != 1 {
			t.Fatalf("rows=%d", h.count())
		}
	})

	t.Run("rate limit counts bad signatures", func(t *testing.T) {
		h := newHarness(t, "rm-rate", allKinds, true)
		h.svc.SetRateLimits(2, 1000)
		body := issueOpened(5)
		bad := "sha256=" + strings.Repeat("cd", 32)
		if h.post("issues", "deliv-rate-1", body, bad, nil) != http.StatusUnauthorized {
			t.Fatal("first bad signature")
		}
		if h.post("issues", "deliv-rate-2", body, bad, nil) != http.StatusUnauthorized {
			t.Fatal("second bad signature")
		}
		if status := h.post("issues", "deliv-rate-3", body, sign(body), nil); status != http.StatusTooManyRequests {
			t.Fatalf("third status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("rate limited request stored %d rows", h.count())
		}
	})

	t.Run("ack is http only and repeat stays handed off", func(t *testing.T) {
		h := newHarness(t, "rm-ack", allKinds, true)
		body := issueOpened(6)
		if h.post("issues", "deliv-ack", body, sign(body), nil) != http.StatusOK {
			t.Fatal("post")
		}
		items := h.list()
		if len(items) != 1 {
			t.Fatal(items)
		}
		id := items[0].ID
		if status := h.ack(h.machine, id); status != http.StatusOK {
			t.Fatalf("ack status=%d", status)
		}
		if h.statusByObject("issue:6") != statusHandedOff {
			t.Fatal(h.statusByObject("issue:6"))
		}
		if status := h.ack(h.machine, id); status != http.StatusOK {
			t.Fatalf("repeat ack status=%d", status)
		}
		if h.statusByObject("issue:6") != statusHandedOff {
			t.Fatal("repeat ack changed status")
		}
		if status := h.ack("someone-else", id); status != http.StatusNotFound {
			t.Fatalf("other machine ack status=%d", status)
		}
		if len(h.list()) != 0 {
			t.Fatal("handed off row still listed as pending")
		}
	})

	t.Run("oversize body is 413 and missing delivery is 400", func(t *testing.T) {
		h := newHarness(t, "rm-size", allKinds, true)
		h.svc.SetMaxBody(8)
		body := []byte(`{"action":"opened","issue":{"number":1}}`)
		if status := h.post("issues", "deliv-big", body, sign(body), nil); status != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversize status=%d", status)
		}
		h.svc.SetMaxBody(defaultMaxWebhookBody)
		if status := h.post("issues", "", issueOpened(1), sign(issueOpened(1)), nil); status != http.StatusBadRequest {
			t.Fatalf("missing delivery status=%d", status)
		}
		if h.count() != 0 {
			t.Fatalf("rows=%d", h.count())
		}
	})
}

func TestSweeperDeletesOldPendingOnly(t *testing.T) {
	h := newHarness(t, "rm-sweep", allKinds, true)
	body := issueOpened(9)
	if h.post("issues", "deliv-sweep", body, sign(body), nil) != http.StatusOK {
		t.Fatal("post")
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format(timeLayout)
	if _, err := h.db.Exec(`UPDATE github_repo_maint_deliveries SET created_at=?`, old); err != nil {
		t.Fatal(err)
	}
	n, err := h.svc.SweepOnce(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || h.count() != 0 {
		t.Fatalf("swept=%d rows=%d", n, h.count())
	}
	fresh := issueOpened(10)
	if h.post("issues", "deliv-sweep-keep", fresh, sign(fresh), nil) != http.StatusOK {
		t.Fatal("second post")
	}
	if _, err := h.db.Exec(`UPDATE github_repo_maint_deliveries SET status=?, created_at=?`, statusHandedOff, old); err != nil {
		t.Fatal(err)
	}
	n, err = h.svc.SweepOnce(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || h.count() != 1 {
		t.Fatalf("handed_off swept=%d rows=%d", n, h.count())
	}
}

func (h *harness) list() []deliveryView {
	h.t.Helper()
	var wrapped struct {
		Deliveries []deliveryView `json:"deliveries"`
	}
	if err := json.Unmarshal([]byte(h.listRaw()), &wrapped); err != nil {
		h.t.Fatal(err)
	}
	return wrapped.Deliveries
}

func (h *harness) listRaw() string {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.url+"/api/github/repo-maint/deliveries?limit=20", nil)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer machine-token")
	req.Header.Set("X-Machine-ID", h.machine)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("list status=%d body=%s", resp.StatusCode, raw)
	}
	return string(raw)
}

func (h *harness) ack(machine, id string) int {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.url+"/api/github/repo-maint/deliveries/"+id+"/ack", nil)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer machine-token")
	req.Header.Set("X-Machine-ID", machine)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func readFrame(t *testing.T, conn *websocket.Conn, want string) json.RawMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	for {
		var env struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("read %s: %v", want, err)
		}
		if env.Type == want {
			return env.Payload
		}
		t.Logf("skipped frame %s", env.Type)
	}
}
