package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordAndListMaclawAppRunHistory(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}

	entry, err := app.RecordMaclawAppRunHistory(maclawAppRunHistoryEntry{
		RunID:          "run-1",
		AppID:          "app-invoice",
		Status:         "done",
		DefinitionHash: "abc123",
		Message:        "ok",
		ArtifactName:   "out.pdf",
		Source:         "runtime",
		At:             "2026-07-15T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("RecordMaclawAppRunHistory() error = %v", err)
	}
	if entry.RunID != "run-1" || entry.AppID != "app-invoice" {
		t.Fatalf("unexpected entry: %+v", entry)
	}

	// Duplicate runID should replace, not duplicate.
	if _, err := app.RecordMaclawAppRunHistory(maclawAppRunHistoryEntry{
		RunID:   "run-1",
		AppID:   "app-invoice",
		Status:  "done",
		Message: "updated",
		At:      "2026-07-15T11:00:00Z",
	}); err != nil {
		t.Fatalf("RecordMaclawAppRunHistory(update) error = %v", err)
	}
	if _, err := app.RecordMaclawAppRunHistory(maclawAppRunHistoryEntry{
		RunID:  "run-2",
		AppID:  "app-invoice",
		Status: "error",
		At:     "2026-07-15T12:00:00Z",
	}); err != nil {
		t.Fatalf("RecordMaclawAppRunHistory(run-2) error = %v", err)
	}

	list, err := app.ListMaclawAppRunHistory("app-invoice", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppRunHistory() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(list))
	}
	if list[0].RunID != "run-2" {
		t.Fatalf("expected newest-first run-2, got %q", list[0].RunID)
	}
	if list[1].Message != "updated" {
		t.Fatalf("expected updated message for run-1, got %q", list[1].Message)
	}

	path := filepath.Join(app.GetDataDir(), "app_run_history.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read durable file: %v", err)
	}
	var store maclawAppRunHistoryStore
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatalf("decode durable file: %v", err)
	}
	if store.Schema != maclawAppRunHistorySchema {
		t.Fatalf("schema = %q", store.Schema)
	}
	if len(store.ByApp["app-invoice"]) != 2 {
		t.Fatalf("store by_app size = %d", len(store.ByApp["app-invoice"]))
	}
}

func TestRecordMaclawAppRunHistoryMergesDuplicateRunID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}

	// Rich runtime entry recorded first (as the frontend does on run completion).
	rich := maclawAppRunHistoryEntry{
		RunID:                      "run-1",
		AppID:                      "app-pdf",
		Status:                     "done",
		DefinitionHash:             "478ef784",
		TestProtocolFingerprint:    "cb949164",
		WorkspaceLayoutFingerprint: "8151d5b1",
		Message:                    "translated.pdf",
		ArtifactName:               "translated.pdf",
		ResultPayload:              map[string]any{"content": "ok"},
		DependencyVerification:     map[string]any{"schema": "maclaw.app.install_plan.v1"},
		Source:                     "runtime",
		At:                         "2026-07-19T13:05:56Z",
	}
	if _, err := app.RecordMaclawAppRunHistory(rich); err != nil {
		t.Fatalf("RecordMaclawAppRunHistory(rich) error = %v", err)
	}
	// Sparse skill-governance stamp for the same run arrives afterwards.
	sparse := maclawAppRunHistoryEntry{
		RunID:              "run-1",
		AppID:              "app-pdf",
		Status:             "done",
		DefinitionHash:     "478ef784",
		ArtifactName:       "translated.pdf",
		SkillName:          "paper_pdf_translator",
		Source:             "skill_governance",
		At:                 "2026-07-19T13:05:57Z",
		Message:            maclawAppSkillGovernanceRunMessage,
		GovernanceRecorded: true,
	}
	if _, err := app.RecordMaclawAppRunHistory(sparse); err != nil {
		t.Fatalf("RecordMaclawAppRunHistory(sparse) error = %v", err)
	}

	list, err := app.ListMaclawAppRunHistory("app-pdf", 10)
	if err != nil {
		t.Fatalf("ListMaclawAppRunHistory() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 merged entry, got %d", len(list))
	}
	got := list[0]
	if got.TestProtocolFingerprint != "cb949164" || got.WorkspaceLayoutFingerprint != "8151d5b1" {
		t.Fatalf("sparse stamp erased evidence fingerprints: %+v", got)
	}
	if got.DependencyVerification == nil || got.ResultPayload == nil {
		t.Fatalf("sparse stamp erased dependency verification / result payload: %+v", got)
	}
	if got.Message != "translated.pdf" {
		t.Fatalf("governance marker should not replace run summary, got %q", got.Message)
	}
	if !got.GovernanceRecorded || got.SkillName != "paper_pdf_translator" {
		t.Fatalf("governance provenance should be preserved: %+v", got)
	}
}

func TestRecordMaclawAppRunHistoryMergesDuplicateRunIDAcrossAppAliases(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	rich := maclawAppRunHistoryEntry{
		RunID: "run-alias-1", AppID: "paper_pdf_translator", Status: "done",
		DefinitionHash: "definition-current", TestProtocolFingerprint: "protocol-current",
		WorkspaceLayoutFingerprint: "layout-current", ResultPayload: map[string]any{"content": "ok"},
		At: "2026-07-30T00:00:00Z",
	}
	if _, err := app.RecordMaclawAppRunHistory(rich); err != nil {
		t.Fatalf("record rich alias evidence: %v", err)
	}
	sparse := maclawAppRunHistoryEntry{
		RunID: "run-alias-1", AppID: "skill-app-paper_pdf_translator-app-pdf", Status: "done",
		DefinitionHash: "definition-current", SkillName: "paper_pdf_translator",
		GovernanceRecorded: true, Message: maclawAppSkillGovernanceRunMessage,
		At: "2026-07-30T00:00:01Z",
	}
	if _, err := app.RecordMaclawAppRunHistory(sparse); err != nil {
		t.Fatalf("record sparse panel evidence: %v", err)
	}
	oldAlias, err := app.ListMaclawAppRunHistory("paper_pdf_translator", 10)
	if err != nil {
		t.Fatalf("list old alias: %v", err)
	}
	if len(oldAlias) != 0 {
		t.Fatalf("same run should move to canonical panel bucket, got %#v", oldAlias)
	}
	canonical, err := app.ListMaclawAppRunHistory("skill-app-paper_pdf_translator-app-pdf", 10)
	if err != nil || len(canonical) != 1 {
		t.Fatalf("canonical history = %#v err=%v", canonical, err)
	}
	got := canonical[0]
	if got.TestProtocolFingerprint != "protocol-current" || got.WorkspaceLayoutFingerprint != "layout-current" || got.ResultPayload == nil {
		t.Fatalf("cross-alias merge lost rich evidence: %+v", got)
	}
}

func TestClearMaclawAppRunHistory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.RecordMaclawAppRunHistory(maclawAppRunHistoryEntry{
		RunID: "r1", AppID: "a1", Status: "done", At: "2026-07-15T10:00:00Z",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	ok, err := app.ClearMaclawAppRunHistory("a1")
	if err != nil {
		t.Fatalf("ClearMaclawAppRunHistory() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected clear to report true")
	}
	list, err := app.ListMaclawAppRunHistory("a1", 10)
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty history, got %d", len(list))
	}
}

func TestRecordMaclawAppRunHistoryRequiresIDs(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.RecordMaclawAppRunHistory(maclawAppRunHistoryEntry{RunID: "r"}); err == nil {
		t.Fatalf("expected missing appID error")
	}
	if _, err := app.RecordMaclawAppRunHistory(maclawAppRunHistoryEntry{AppID: "a"}); err == nil {
		t.Fatalf("expected missing runID error")
	}
}

func TestCheckMaclawAppRuntimeHealthReady(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	// Minimal tool app with no required skill dependencies should plan successfully.
	pkg := `{
		"schema":"maclaw.app.v1",
		"privateMarker":"x_maclaw_apps",
		"app":{
			"id":"hello-app",
			"name":"Hello",
			"kind":"tool_app",
			"version":1,
			"binding":{
				"skill":{"id":"hello","source":"local"}
			}
		}
	}`
	health, err := app.CheckMaclawAppRuntimeHealth(pkg, "hello-app")
	if err != nil {
		t.Fatalf("CheckMaclawAppRuntimeHealth() error = %v", err)
	}
	if health["schema"] != "maclaw.app.runtime_health.v1" {
		t.Fatalf("schema = %v", health["schema"])
	}
	if _, ok := health["plan"]; !ok {
		t.Fatalf("expected plan in health response")
	}
	if _, ok := health["ok"].(bool); !ok {
		t.Fatalf("expected ok bool, got %T", health["ok"])
	}
}

func TestRecordMaclawAppRunHistoryConcurrentWrites(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	const n = 20
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			_, err := app.RecordMaclawAppRunHistory(maclawAppRunHistoryEntry{
				RunID:  fmt.Sprintf("run-%d", i),
				AppID:  "concurrent-app",
				Status: "done",
				At:     fmt.Sprintf("2026-07-15T12:%02d:00Z", i),
			})
			errCh <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent record error: %v", err)
		}
	}
	list, err := app.ListMaclawAppRunHistory("concurrent-app", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != n {
		t.Fatalf("expected %d entries after concurrent writes, got %d", n, len(list))
	}
}

func TestMaclawAppDependencyIsHardBlockedAlignsWithInstallableAction(t *testing.T) {
	installable := maclawAppInstallPlanDependency{
		ID: "skill-a", Required: true, Installed: false, Health: "missing", Action: "install",
	}
	if maclawAppDependencyIsHardBlocked(installable) {
		t.Fatalf("action=install must not be hard-blocked")
	}
	blocked := maclawAppInstallPlanDependency{
		ID: "skill-b", Required: true, Installed: false, Health: "missing", Action: "blocked",
	}
	if !maclawAppDependencyIsHardBlocked(blocked) {
		t.Fatalf("action=blocked must be hard-blocked")
	}
	disabled := maclawAppInstallPlanDependency{
		ID: "skill-c", Required: true, Installed: true, Health: "disabled", Action: "blocked",
	}
	if !maclawAppDependencyIsHardBlocked(disabled) {
		t.Fatalf("disabled installed skill must be hard-blocked")
	}
}

func TestCheckMaclawAppRuntimeHealthIgnoresStaleInstallRecordBlocking(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	// Seed an install record that still claims blocking deps after a repair.
	// Package itself has no skill dependencies so the live plan is clean.
	registry := maclawAppInstallRegistry{
		Schema: "maclaw.app.installs.v1",
		Installs: []maclawAppInstallRecord{{
			AppID:                 "normal-app",
			AppName:               "Normal App",
			Kind:                  "enterprise_normal_app",
			Source:                "market",
			HasMissingRequired:    true,
			HasBlockingDependency: true,
			Message:               "stale snapshot",
		}},
	}
	if err := app.writeMaclawAppInstallRegistry(registry); err != nil {
		t.Fatalf("write install registry: %v", err)
	}
	pkg := `{
		"schema":"maclaw.app.v1",
		"privateMarker":"x_maclaw_apps",
		"app":{
			"id":"normal-app",
			"name":"Normal App",
			"kind":"enterprise_normal_app",
			"version":1,
			"binding":{
				"datasrv":{"domain":"tools","datasetID":"tools.runs","preferredView":"tools.list"}
			}
		}
	}`
	health, err := app.CheckMaclawAppRuntimeHealth(pkg, "normal-app")
	if err != nil {
		t.Fatalf("CheckMaclawAppRuntimeHealth() error = %v", err)
	}
	if health["ok"] != true {
		t.Fatalf("live plan should win over stale install record: %#v", health)
	}
	if health["blocked"] != false {
		t.Fatalf("blocked should be false, got %#v", health["blocked"])
	}
	if health["install_record_present"] != true || health["install_record_blocking"] != true {
		t.Fatalf("install record snapshot fields should still surface: %#v", health)
	}
}

func TestListAllMaclawAppRunHistory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	for _, item := range []maclawAppRunHistoryEntry{
		{RunID: "a-1", AppID: "app-a", Status: "done", At: "2026-07-15T09:00:00Z"},
		{RunID: "b-1", AppID: "app-b", Status: "done", At: "2026-07-15T10:00:00Z"},
		{RunID: "a-2", AppID: "app-a", Status: "error", At: "2026-07-15T11:00:00Z"},
	} {
		if _, err := app.RecordMaclawAppRunHistory(item); err != nil {
			t.Fatalf("record %s: %v", item.RunID, err)
		}
	}
	all, err := app.ListAllMaclawAppRunHistory(10)
	if err != nil {
		t.Fatalf("ListAllMaclawAppRunHistory() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	if all[0].RunID != "a-2" {
		t.Fatalf("expected newest a-2 first, got %q", all[0].RunID)
	}
}
