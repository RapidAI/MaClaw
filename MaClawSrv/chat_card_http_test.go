package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestChatCardDecisionResumesSameTask(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	if server == nil || server.chatCards == nil {
		t.Fatal("interaction store was not opened")
	}
	defer server.Close()
	ctx := context.Background()
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(ctx, principal, testLLMConfig()); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(ctx, principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatal(err)
	}
	_, run, _, err := svc.SendMessage(ctx, principal, inst.ID, agentservice.SendMessageInput{
		Content:  "打开订单页",
		Metadata: map[string]string{"bot_id": "bot-a"},
	})
	if err != nil || run == nil {
		t.Fatal(err)
	}
	other, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	shot := base64.StdEncoding.EncodeToString([]byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x5f, 0xec, 0x8c,
		0x6a, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	})
	opened := postJSON(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions", map[string]any{
		"kind": "native_handoff", "question": "请在这台电脑上完成登录",
		"lease_id": "lease-a", "desktop_mode": "isolated", "shot_base64": shot,
		"target": map[string]string{"url": "https://shop.example/pay", "origin": "shop.example"},
	}, http.StatusOK)
	if opened["spotlight"] == true || opened["done"] == true || opened["handoff"] != "full_page" {
		t.Fatalf("opened = %#v", opened)
	}
	shotView, _ := opened["shot"].(map[string]any)
	if shotView == nil || shotView["lease_id"] != "lease-a" || shotView["desktop_mode"] != "isolated" {
		t.Fatalf("shot = %#v", opened["shot"])
	}
	cardID, _ := opened["id"].(string)
	taskID, _ := opened["task_id"].(string)
	if _, _, _, err := svc.SendMessage(ctx, principal, inst.ID, agentservice.SendMessageInput{SessionID: run.SessionID, Content: "确认"}); err != nil {
		t.Fatal(err)
	}
	still := getJSON(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID, http.StatusOK)
	if still["status"] != "pending" || still["resume_count"] != float64(0) {
		t.Fatalf("free text resolved the card: %#v", still)
	}
	bare := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/decision", bytes.NewBufferString(`{}`))
	bareW := httptest.NewRecorder()
	server.Handler().ServeHTTP(bareW, bare)
	if bareW.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", bareW.Code)
	}
	wrong := postJSON(t, server, otherToken, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/decision", map[string]any{
		"task_id": taskID, "bot_id": "bot-a", "choice_ids": []string{"completed"},
		"operation_version": opened["operation"].(map[string]any)["version"],
		"payload_hash":      opened["operation"].(map[string]any)["hash"],
	}, 0)
	_ = wrong
	if got := postStatus(t, server, otherToken, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/decision", map[string]any{
		"task_id": taskID, "bot_id": "bot-a", "choice_ids": []string{"completed"},
		"operation_version": opened["operation"].(map[string]any)["version"],
		"payload_hash":      opened["operation"].(map[string]any)["hash"],
		"lease_id":          "lease-a", "shot_id": shotView["id"],
	}); got == http.StatusOK {
		t.Fatal("other user resumed the task")
	}
	tamper := postStatus(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/decision", map[string]any{
		"task_id": taskID, "bot_id": "bot-a", "choice_ids": []string{"pay-now"},
		"operation_version": opened["operation"].(map[string]any)["version"],
		"payload_hash":      opened["operation"].(map[string]any)["hash"],
		"lease_id":          "lease-a", "shot_id": shotView["id"], "captured_at": shotView["captured_at"],
	})
	if tamper == http.StatusOK {
		t.Fatal("tampered choice resumed the task")
	}
	stale := postStatus(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/decision", map[string]any{
		"task_id": taskID, "bot_id": "bot-a", "choice_ids": []string{"completed"},
		"operation_version": 9, "payload_hash": "nope",
		"lease_id": "lease-a", "shot_id": shotView["id"],
	})
	if stale == http.StatusOK {
		t.Fatal("stale version resumed the task")
	}
	siblingRun := openSibling(t, svc, principal, inst.ID)
	sibling := postJSON(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+siblingRun.ID+"/interactions", map[string]any{
		"kind": "native_handoff", "question": "另一台电脑",
		"lease_id": "lease-b", "desktop_mode": "shared",
	}, http.StatusOK)
	if postStatus(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/desktop", map[string]any{
		"bot_id": "bot-a", "lease_id": "lease-b", "captured_at": shotView["captured_at"],
	}) == http.StatusOK {
		t.Fatal("sibling lease opened this computer")
	}
	desk := postJSON(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/desktop", map[string]any{
		"bot_id": "bot-a", "lease_id": "lease-a", "captured_at": shotView["captured_at"],
	}, http.StatusOK)
	if desk["wake"] == true || desk["attach"] != "watch" || desk["bot_id"] != "bot-a" || desk["lease_id"] != "lease-a" || desk["desktop_mode"] != "isolated" {
		t.Fatalf("desktop = %#v", desk)
	}
	op := opened["operation"].(map[string]any)
	decided := postJSON(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/decision", map[string]any{
		"task_id": taskID, "bot_id": "bot-a", "choice_ids": []string{"completed"},
		"operation_version": op["version"], "payload_hash": op["hash"],
		"lease_id": "lease-a", "shot_id": shotView["id"], "captured_at": shotView["captured_at"],
		"target_origin": "shop.example", "target_url": "https://shop.example/pay",
	}, http.StatusOK)
	if decided["resume"] != true || decided["done"] == true || decided["mode"] != "verify" || decided["run_id"] != run.ID || decided["task_id"] != taskID {
		t.Fatalf("decision = %#v", decided)
	}
	if granted, _ := decided["granted"].([]any); len(granted) != 0 {
		t.Fatalf("granted = %#v", decided["granted"])
	}
	waitOutcome(t, svc, principal, inst.ID, run.ID)
	again := postJSON(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+run.ID+"/interactions/"+cardID+"/decision", map[string]any{
		"task_id": taskID, "bot_id": "bot-a", "choice_ids": []string{"completed"},
		"operation_version": op["version"], "payload_hash": op["hash"],
		"lease_id": "lease-a", "shot_id": shotView["id"], "captured_at": shotView["captured_at"],
	}, http.StatusOK)
	if again["resume"] == true || again["already"] != true || again["run_id"] != run.ID {
		t.Fatalf("repeat = %#v", again)
	}
	if sibling["status"] != "pending" {
		t.Fatalf("sibling changed at open: %#v", sibling)
	}
	siblingNow := getJSON(t, server, token, "/api/v1/instances/"+inst.ID+"/runs/"+siblingRun.ID+"/interactions/"+sibling["id"].(string), http.StatusOK)
	if siblingNow["status"] != "pending" {
		t.Fatalf("sibling = %#v", siblingNow)
	}

	ctxShot, slot := withDesktopShotSlot(context.Background())
	slot.mu.Lock()
	slot.ready = true
	slot.shot = agent.MessageAttachment{MimeType: "image/png", Data: shot}
	slot.mu.Unlock()
	scope := agentruntime.Scope{TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: run.SessionID, RunID: "takeover-run"}
	if _, err := requestDesktopTakeover(ctxShot, scope, "请在这台电脑上登录"); err != nil {
		t.Fatal(err)
	}
	bound, err := desktopChatCards.FindByRun(tenant.ID, user.ID, "takeover-run")
	if err != nil || bound.Shot == nil || bound.LeaseID != user.ID || bound.DesktopMode != "shared" || bound.Kind != "native_handoff" {
		t.Fatalf("takeover card = %#v %v", bound, err)
	}
	if _, _, err := desktopChatCards.ShotBytes(chatActor(principal), bound.ID); err != nil {
		t.Fatal(err)
	}
}

func openSibling(t *testing.T, svc *agentservice.Service, p agentservice.Principal, instanceID string) *agentservice.Run {
	t.Helper()
	_, run, _, err := svc.SendMessage(context.Background(), p, instanceID, agentservice.SendMessageInput{
		Content:  "另一件事",
		Metadata: map[string]string{"bot_id": "bot-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func waitOutcome(t *testing.T, svc *agentservice.Service, p agentservice.Principal, instanceID, runID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := svc.GetRun(context.Background(), p, instanceID, runID)
		if err == nil && run.Metadata["task_outcome"] == agentservice.TaskOutcomeUnverified {
			if run.Metadata["task_outcome"] == "done" {
				t.Fatal("card continuation was marked done")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	run, _ := svc.GetRun(context.Background(), p, instanceID, runID)
	t.Fatalf("run outcome = %#v", run)
}

func postJSON(t *testing.T, server *HTTPServer, token, path string, body any, want int) map[string]any {
	t.Helper()
	status, decoded := doJSON(t, server, token, http.MethodPost, path, body)
	if want != 0 && status != want {
		t.Fatalf("%s status = %d body = %#v", path, status, decoded)
	}
	return decoded
}

func postStatus(t *testing.T, server *HTTPServer, token, path string, body any) int {
	t.Helper()
	status, _ := doJSON(t, server, token, http.MethodPost, path, body)
	return status
}

func getJSON(t *testing.T, server *HTTPServer, token, path string, want int) map[string]any {
	t.Helper()
	status, decoded := doJSON(t, server, token, http.MethodGet, path, nil)
	if status != want {
		t.Fatalf("GET %s status = %d body = %#v", path, status, decoded)
	}
	return decoded
}

func doJSON(t *testing.T, server *HTTPServer, token, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	var decoded map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &decoded)
	return w.Code, decoded
}
