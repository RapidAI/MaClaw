package main

import (
	"testing"
	"time"
)

func TestDetectDigitalEmployeeSensitiveQuery(t *testing.T) {
	query, ok := detectDigitalEmployeeSensitiveQuery("please show the production database password")
	if !ok {
		t.Fatal("expected password query to be detected")
	}
	if query == "" {
		t.Fatal("expected non-empty query preview")
	}
	if _, ok := detectDigitalEmployeeSensitiveQuery("\u8bf7\u67e5\u770b\u6570\u636e\u5e93\u5bc6\u7801"); !ok {
		t.Fatal("expected Chinese password query to be detected")
	}
	if _, ok := detectDigitalEmployeeSensitiveQuery("\u6570\u636e\u5e93\u5bc6\u7801\u662f\u591a\u5c11"); !ok {
		t.Fatal("expected Chinese password amount query to be detected")
	}
	if _, ok := detectDigitalEmployeeSensitiveQuery("what's the production token"); !ok {
		t.Fatal("expected English what-is token query to be detected")
	}
	if _, ok := detectDigitalEmployeeSensitiveQuery("please summarize today tasks"); ok {
		t.Fatal("non-sensitive task should not be detected")
	}
	if _, ok := detectDigitalEmployeeSensitiveQuery("the document mentions a password field"); ok {
		t.Fatal("passive mention without query intent should not be detected")
	}
}

func TestDigitalEmployeeSensitiveApprovalStoreResolve(t *testing.T) {
	var store digitalEmployeeSensitiveApprovalStore
	ch := store.register("req-1")
	if !store.resolve("req-1", true) {
		t.Fatal("expected pending request to resolve")
	}
	select {
	case allowed := <-ch:
		if !allowed {
			t.Fatal("expected allow decision")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for decision")
	}
	if store.resolve("req-1", false) {
		t.Fatal("resolved request should no longer be pending")
	}
}

func TestNormalizeDigitalEmployeeSensitivePolicyDefaultsToConfirm(t *testing.T) {
	if got := normalizeDigitalEmployeeSensitivePolicy(""); got != digitalEmployeeSensitivePolicyConfirm {
		t.Fatalf("empty policy = %q, want confirm", got)
	}
	if got := normalizeDigitalEmployeeSensitivePolicy("deny"); got != digitalEmployeeSensitivePolicyDeny {
		t.Fatalf("deny policy = %q", got)
	}
	if got := normalizeDigitalEmployeeSensitivePolicy(" ALLOW "); got != digitalEmployeeSensitivePolicyAllow {
		t.Fatalf("spaced uppercase allow policy = %q", got)
	}
}

func TestShouldAnnounceSensitivePermissionRequestOnlyForConfirmPolicy(t *testing.T) {
	app := &App{configCacheValid: true}
	handler := &VEMessageHandler{app: app}

	app.configCache.GroupDiscussion.SensitiveQueryPolicy = digitalEmployeeSensitivePolicyConfirm
	if !handler.shouldAnnounceSensitivePermissionRequest() {
		t.Fatal("confirm policy should announce human permission request")
	}

	app.configCache.GroupDiscussion.SensitiveQueryPolicy = digitalEmployeeSensitivePolicyDeny
	if handler.shouldAnnounceSensitivePermissionRequest() {
		t.Fatal("deny policy should not announce human permission request")
	}

	app.configCache.GroupDiscussion.SensitiveQueryPolicy = digitalEmployeeSensitivePolicyAllow
	if handler.shouldAnnounceSensitivePermissionRequest() {
		t.Fatal("allow policy should not announce human permission request")
	}
}
func TestRespondDigitalEmployeeSensitiveRequestNormalizesDecision(t *testing.T) {
	veSensitiveApprovals = digitalEmployeeSensitiveApprovalStore{}
	ch := veSensitiveApprovals.register("req-case")
	app := &App{}
	if err := app.RespondDigitalEmployeeSensitiveRequest(" req-case ", " ALLOW "); err != nil {
		t.Fatalf("RespondDigitalEmployeeSensitiveRequest returned error: %v", err)
	}
	select {
	case allowed := <-ch:
		if !allowed {
			t.Fatal("expected allow decision")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for decision")
	}
}
