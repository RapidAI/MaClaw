package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func newRefreshOnlyMaintenanceHandler(t *testing.T, refresh func() error) *IMMessageHandler {
	t.Helper()
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: t.TempDir()}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.toolRouter = NewToolRouter(nil)
	app.toolRouter.refreshSkillIndexOverride = refresh
	entry := corelib.NLSkillEntry{
		Name:         "refresh-only",
		Source:       "manual",
		Status:       "active",
		SkillDir:     t.TempDir(),
		LastRepairAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := app.skillExecutor.Register(entry); err != nil {
		t.Fatalf("register refresh-only skill: %v", err)
	}
	t.Cleanup(func() { app.shutdown(context.Background()) })
	return &IMMessageHandler{app: app}
}

func decodeMaintenancePayload(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("maintenance payload is not JSON: %v (%s)", err, raw)
	}
	return payload
}

func TestToolExecuteSkillMaintenancePlanRefreshIndexOnlyCommitsCheckedRefresh(t *testing.T) {
	refreshCalls := 0
	h := newRefreshOnlyMaintenanceHandler(t, func() error {
		refreshCalls++
		return nil
	})
	payload := decodeMaintenancePayload(t, h.toolExecuteSkillMaintenancePlan(map[string]interface{}{
		"dry_run":          false,
		"confirm":          true,
		"approved_actions": []interface{}{cskill.MaintenanceActionRefreshIndex},
	}))
	if ok, _ := payload["ok"].(bool); !ok || payload["state"] != "committed" || payload["cleanup_status"] != "clear" {
		t.Fatalf("refresh-only result = %#v, want committed/clear", payload)
	}
	if refreshCalls != 1 {
		t.Fatalf("checked refresh calls = %d, want 1", refreshCalls)
	}
}

func TestToolExecuteSkillMaintenancePlanRefreshIndexOnlyFailsClosedOnProviderError(t *testing.T) {
	h := newRefreshOnlyMaintenanceHandler(t, func() error { return errors.New("injected maintenance index failure") })
	payload := decodeMaintenancePayload(t, h.toolExecuteSkillMaintenancePlan(map[string]interface{}{
		"dry_run":          false,
		"confirm":          true,
		"approved_actions": []interface{}{cskill.MaintenanceActionRefreshIndex},
	}))
	if ok, _ := payload["ok"].(bool); ok || payload["state"] != "rolled_back" || payload["cleanup_status"] != "clear" {
		t.Fatalf("refresh-only failure result = %#v, want rolled_back/clear", payload)
	}
	if !strings.Contains(payload["error"].(string), "refresh Skill index") {
		t.Fatalf("refresh-only failure error = %#v", payload["error"])
	}
}

func TestToolExecuteSkillMaintenancePlanReviewAuditFailureIsNotReportedOK(t *testing.T) {
	h := newRefreshOnlyMaintenanceHandler(t, func() error { return nil })
	original := recordSkillDraftExecutionAuditForMaintenance
	recordSkillDraftExecutionAuditForMaintenance = func(*IMMessageHandler, []string, string, string) error {
		return errors.New("injected review execution audit failure")
	}
	t.Cleanup(func() { recordSkillDraftExecutionAuditForMaintenance = original })
	payload := decodeMaintenancePayload(t, h.toolExecuteSkillMaintenancePlan(map[string]interface{}{
		"dry_run":          false,
		"confirm":          true,
		"approved_actions": []interface{}{cskill.MaintenanceActionRefreshIndex},
	}))
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("review audit failure reported ok=true: %#v", payload)
	}
	if payload["state"] != "committed" || payload["cleanup_status"] != "clear" {
		t.Fatalf("review audit failure state = %#v, want committed/clear with explicit error", payload)
	}
	if payload["failure_reason"] != "review_execution_audit_failed" || !strings.Contains(payload["error"].(string), "review execution audit failed") {
		t.Fatalf("review audit failure diagnostics = %#v", payload)
	}
}

func TestToolExecuteSkillMaintenancePlanNoOpStillBlocksOnUnreadableCompensationQueue(t *testing.T) {
	h := newRefreshOnlyMaintenanceHandler(t, func() error { return nil })
	queuePath := cskill.DefaultEvolutionCompensationPath()
	if err := os.MkdirAll(filepath.Dir(queuePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queuePath, []byte("{malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := decodeMaintenancePayload(t, h.toolExecuteSkillMaintenancePlan(map[string]interface{}{
		"dry_run":          false,
		"confirm":          true,
		"approved_actions": []interface{}{cskill.MaintenanceActionRefreshIndex},
	}))
	if ok, _ := payload["ok"].(bool); ok || payload["failure_reason"] != "compensation_queue_unavailable" {
		t.Fatalf("queue-blocked no-op result = %#v, want fail-closed compensation_queue_unavailable", payload)
	}
}
