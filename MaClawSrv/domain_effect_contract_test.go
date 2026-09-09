package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestRecordedDomainEffectCommitsPrivateReceiptWithoutJobLeak(t *testing.T) {
	m := newAsyncJobManager(t.TempDir())
	t.Cleanup(m.close)
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	job := m.createUserJob("mcp.create", p, func(ctx context.Context) (any, error) {
		return executeRecordedJobEffect(ctx, "mcp.create", map[string]any{"operation": "create", "secret": "must-stay-private"}, false, func(context.Context) (any, string, error) {
			return map[string]any{"id": "mcp-1", "name": "demo"}, "mcp-1", nil
		})
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := m.getUserJob(job.ID, p)
		if ok && current.Status == asyncJobStatusSucceeded {
			effects, err := m.effectRepository.ListByJob(context.Background(), job.ID)
			if err != nil || len(effects) != 1 {
				t.Fatalf("effects=%#v err=%v", effects, err)
			}
			if effects[0].State != agentruntime.JobEffectCommitted || effects[0].ResourceID != "mcp-1" {
				t.Fatalf("unexpected effect: %#v", effects[0])
			}
			if string(effects[0].Payload) == "" || string(effects[0].Payload) == "null" {
				t.Fatal("committed effect did not retain replay payload")
			}
			if string(current.Result) == "" || !json.Valid(current.Result) {
				t.Fatalf("invalid job result: %s", current.Result)
			}
			if string(current.Result) == "" || string(current.Result) == "null" {
				t.Fatalf("empty result projection: %s", current.Result)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for recorded effect job")
}

func TestDomainReconcilerSanitizesCommittedPayload(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir()}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	server := &HTTPServer{svc: svc}
	payload := json.RawMessage(`{"resource_id":"mcp-1","endpoint":"https://user:secret@example.test/api?token=top-secret","path":"` + filepath.ToSlash(svc.DataRoot()) + `/private"}`)
	result, err := (mcpJobReconciler{server: server}).ReconcileJob(context.Background(), agentruntime.Job{ID: "job", Kind: "mcp.create"}, []agentruntime.JobEffect{{Kind: "mcp.create", State: agentruntime.JobEffectCommitted, Payload: payload}})
	if err != nil || !result.Resolved || result.Status != agentruntime.JobStatusSucceeded {
		t.Fatalf("reconcile result=%#v err=%v", result, err)
	}
	if strings.Contains(string(result.Result), "top-secret") || strings.Contains(string(result.Result), "user:secret") || strings.Contains(string(result.Result), svc.DataRoot()) {
		t.Fatalf("committed payload leaked sensitive data: %s", result.Result)
	}
}

func TestKnowledgeReconcilerResolvesExistingResourceAndRejectsMismatchedEffect(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir()}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source, err := store.SaveText(context.Background(), knowledge.TextSaveRequest{TenantID: "tenant", OwnerID: "user", Text: "runbook", Title: "Runbook"})
	if err != nil {
		t.Fatal(err)
	}
	server := &HTTPServer{svc: svc, knowledgeMgr: &knowledgeStoreManager{store: store}}
	job := agentruntime.Job{ID: "job", Kind: "public_knowledge_import_text", TenantID: "tenant", UserID: "user"}
	resolved, err := (knowledgeJobReconciler{server: server}).ReconcileJob(context.Background(), job, []agentruntime.JobEffect{{Kind: "knowledge.public.import.text", State: agentruntime.JobEffectPrepared, ResourceID: source.ID}})
	if err != nil || !resolved.Resolved || resolved.Status != agentruntime.JobStatusSucceeded || !strings.Contains(string(resolved.Result), source.ID) {
		t.Fatalf("existing resource was not reconciled: %#v err=%v", resolved, err)
	}
	unresolved, err := (knowledgeJobReconciler{server: server}).ReconcileJob(context.Background(), job, []agentruntime.JobEffect{{Kind: "knowledge.import.text", State: agentruntime.JobEffectCommitted, Payload: json.RawMessage(`{"source_id":"wrong-kind"}`)}})
	if err != nil || unresolved.Resolved {
		t.Fatalf("mismatched effect kind should remain unresolved: %#v err=%v", unresolved, err)
	}
}

func TestDomainJobReconcilerMapsPublicKnowledgeText(t *testing.T) {
	server := &HTTPServer{}
	if reconciler := domainJobReconcilerFor(server, "public_knowledge_import_text"); reconciler == nil {
		t.Fatal("public knowledge text jobs must use the knowledge reconciler")
	}
}

func TestDomainJobReconcilerCoversReconcileAdmissionKinds(t *testing.T) {
	server := &HTTPServer{}
	for _, kind := range []string{
		"mcp.create", "mcp.update", "mcp.start", "mcp.stop",
		"skill.install", "skill.import", "skill.upload",
		"knowledge_import_file", "knowledge_import_urls", "knowledge_import_directory",
		"knowledge_import_share", "knowledge_import_package",
		"public_knowledge_import_urls", "public_knowledge_import_file", "public_knowledge_import_text",
		"migration.export", "migration.import",
	} {
		if reconciler := domainJobReconcilerFor(server, kind); reconciler == nil {
			t.Errorf("reconcile-policy job %q has no domain reconciler", kind)
		}
	}
	if reconciler := domainJobReconcilerFor(server, "mcp.health_check"); reconciler != nil {
		t.Fatal("fail-policy health checks must not grow a replay reconciler")
	}
}

func TestRunEventAPISanitizationPreservesCorrelationAndRedactsSecrets(t *testing.T) {
	root := t.TempDir()
	event := agentservice.RunEvent{Type: "tool.result", Payload: map[string]any{
		"request_id": "req-1", "endpoint": "https://user:pw@example.test/tool?api_key=secret", "path": filepath.Join(root, "private"), "ok": true,
	}}
	out := sanitizeRunEventForAPI(root, event)
	encoded, _ := json.Marshal(out.Payload)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "user:pw") || strings.Contains(string(encoded), root) {
		t.Fatalf("run event leaked sensitive payload: %s", encoded)
	}
	if out.Payload["request_id"] != "req-1" || out.Payload["ok"] != true {
		t.Fatalf("run event lost safe fields: %#v", out.Payload)
	}
}
