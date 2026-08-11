package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

func TestHTTPServerInitializesSharedCodingRuntimeLedger(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	server := NewHTTPServer(svc, "admin-secret", nil)
	defer server.Close()
	if server.codingRuntimeStore == nil {
		t.Fatal("MaClawSrv did not initialize corelib coding runtime ledger")
	}
	if _, err := server.codingRuntimeStore.CreateTask(codingruntime.Task{ProjectRef: "server-workspace"}); err != nil {
		t.Fatal(err)
	}
}

func TestCodingRuntimeRecoveryAPIConfirmsWithoutReplayingExecutor(t *testing.T) {
	const tokenSecret = "test-token-secret-0123456789012345"
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: tokenSecret}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance", AllowInvalidConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Coding"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := agentservice.NewTokenManager(tokenSecret, time.Hour).Issue(principal)
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	defer server.Close()
	// The API must remain fail-closed when the host workspace cannot be
	// inspected. A missing/non-Git workspace exercises that policy without
	// depending on a machine-specific Git installation in the HTTP test.
	policy := codingruntime.PolicySnapshot{ProjectRoot: inst.Workspace, Mode: "local", FinalWorkspaceGateRequired: true}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Digest = digest
	task, err := server.codingRuntimeStore.CreateTask(codingruntime.Task{
		TaskID: "recovery-task", OwnerID: "srv:" + tenant.ID + ":" + user.ID + ":" + session.ID,
		ProjectRef: inst.Workspace, Mode: "local", PolicyDigest: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := server.codingRuntimeStore.StartAttempt(task.TaskID, task.OwnerID, time.Minute, policy, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.codingRuntimeStore.FinishAttempt(attempt.AttemptID, task.OwnerID, codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/instances/" + inst.ID + "/sessions/" + session.ID + "/coding-runtime/" + task.TaskID + "/recovery"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("review status=%d body=%s", w.Code, w.Body.String())
	}
	// Confirm still needs a review digest, so reject before any continuation
	// mutation and prove the interrupted task cannot be replayed by this API.
	body := strings.NewReader(`{"review_digest":"sha256:missing-review","confirmed":true}`)
	req = httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("confirm status=%d body=%s", w.Code, w.Body.String())
	}
	if current, err := server.codingRuntimeStore.GetTask(task.TaskID); err != nil || current.Status != codingruntime.TaskInterrupted {
		t.Fatalf("failed recovery mutated task=%#v err=%v", current, err)
	}
	if attempts, err := server.codingRuntimeStore.ListAttempts(task.TaskID); err != nil || len(attempts) != 1 || attempts[0].Status != codingruntime.TaskInterrupted {
		t.Fatalf("attempt was replayed: %#v err=%v", attempts, err)
	}
}

func TestCodingRuntimeRecoveryAPIQueuesConfirmedAttemptWithoutReplay(t *testing.T) {
	const tokenSecret = "test-token-secret-0123456789012345"
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: tokenSecret}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance", AllowInvalidConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Coding"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := agentservice.NewTokenManager(tokenSecret, time.Hour).Issue(principal)
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	defer server.Close()
	server.codingRuntimeRecoveryProber = func(task codingruntime.Task) codingruntime.WorkspaceProber {
		return codingruntime.WorkspaceProberFunc(func(context.Context, codingruntime.Task, codingruntime.Attempt) (*codingruntime.WorkspaceProbe, error) {
			return &codingruntime.WorkspaceProbe{ProjectRef: task.ProjectRef, Head: "head", StatusHash: "status", FilesHash: "files", WorkDir: task.ProjectRef}, nil
		})
	}
	policy := codingruntime.PolicySnapshot{ProjectRoot: inst.Workspace, Mode: "local", FinalWorkspaceGateRequired: true}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.Digest = digest
	task, err := server.codingRuntimeStore.CreateTask(codingruntime.Task{TaskID: "recovery-queue-task", OwnerID: "srv:" + tenant.ID + ":" + user.ID + ":" + session.ID, ProjectRef: inst.Workspace, Mode: "local", PolicyDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := server.codingRuntimeStore.StartAttempt(task.TaskID, task.OwnerID, time.Minute, policy, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.codingRuntimeStore.FinishAttempt(attempt.AttemptID, task.OwnerID, codingruntime.FinishInput{Status: codingruntime.TaskInterrupted, SideEffectState: codingruntime.SideEffectUncertain}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/instances/" + inst.ID + "/sessions/" + session.ID + "/coding-runtime/" + task.TaskID + "/recovery"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%s", w.Code, w.Body.String())
	}
	var review codingRuntimeRecoveryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.ReviewDigest == "" || review.TaskID != task.TaskID || review.AttemptID != attempt.AttemptID {
		t.Fatalf("unexpected review: %#v", review)
	}
	body := strings.NewReader(`{"review_digest":"` + review.ReviewDigest + `","confirmed":true}`)
	req = httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "queued_without_replay") {
		t.Fatalf("confirm status=%d body=%s", w.Code, w.Body.String())
	}
	if current, err := server.codingRuntimeStore.GetTask(task.TaskID); err != nil || current.Status != codingruntime.TaskQueued {
		t.Fatalf("confirmed recovery did not queue task=%#v err=%v", current, err)
	}
	if attempts, err := server.codingRuntimeStore.ListAttempts(task.TaskID); err != nil || len(attempts) != 1 || attempts[0].Status != codingruntime.TaskInterrupted {
		t.Fatalf("recovery replayed attempt: %#v err=%v", attempts, err)
	}
}

func TestAuthorizeRemoteCodingRuntimeRecoveryBindsInstanceSessionAndTarget(t *testing.T) {
	p := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	inst := agentservice.Instance{ID: "instance", Workspace: t.TempDir()}
	session := agentservice.Session{ID: "session"}
	policy := codingruntime.PolicySnapshot{Mode: "remote", ProjectRoot: "/srv/app", RemoteTarget: "sha256:pinned-target"}
	task := codingruntime.Task{OwnerID: "srv:tenant:user:session", WorkflowID: "workflow", PhaseID: "implementation", Mode: "remote", ProjectRef: "/srv/app"}
	task.TaskID = serviceCodingRuntimeTaskIDForRecovery(p, inst, session, task, policy)
	if err := authorizeServiceCodingRuntimeRecovery(p, inst, session, task, policy); err != nil {
		t.Fatalf("authorized remote recovery rejected: %v", err)
	}
	wrongTarget := policy
	wrongTarget.RemoteTarget = "sha256:other-target"
	if err := authorizeServiceCodingRuntimeRecovery(p, inst, session, task, wrongTarget); err == nil {
		t.Fatal("remote recovery accepted a different pinned target")
	}
	wrongSession := session
	wrongSession.ID = "other-session"
	if err := authorizeServiceCodingRuntimeRecovery(p, inst, wrongSession, task, policy); err == nil {
		t.Fatal("remote recovery accepted a different session")
	}
}

func TestRemoteCodingRuntimeMetadataResolvesPinnedConfiguredHostOnly(t *testing.T) {
	hosts := []corelib.SSHHostEntry{{
		Label: "build", Host: "BUILD.Example.Test", User: "deploy", Port: 2222, HostKeyFingerprint: "SHA256:fixed-pin",
	}}
	target, err := configuredRemoteCodingTarget(hosts, " BUILD ", "/srv/app/../app")
	if err != nil {
		t.Fatal(err)
	}
	if target.Host != "build.example.test" || target.User != "deploy" || target.Port != 2222 || target.WorkDir != "/srv/app" || target.HostKeyFingerprint != "SHA256:fixed-pin" {
		t.Fatalf("configured target = %#v", target)
	}
	if _, err := configuredRemoteCodingTarget(hosts, "build", "/srv/app"); err != nil {
		t.Fatal(err)
	}
	if _, err := configuredRemoteCodingTarget([]corelib.SSHHostEntry{{Label: "build", Host: "build.example.test", User: "deploy"}}, "build", "/srv/app"); err == nil {
		t.Fatal("unpinned SSH host was admitted to remote coding runtime")
	}
	if _, err := configuredRemoteCodingTarget(append(hosts, hosts[0]), "build", "/srv/app"); err == nil {
		t.Fatal("ambiguous SSH label was admitted to remote coding runtime")
	}
}

func TestGenericMessageEndpointRejectsCallerSuppliedCodingRuntimeMetadata(t *testing.T) {
	if !isReservedCodingRuntimeMetadata(map[string]string{"coding_runtime_mode": "remote_workflow"}) {
		t.Fatal("runtime metadata was not recognized as reserved")
	}
	if isReservedCodingRuntimeMetadata(map[string]string{"mutation_scope": "project"}) {
		t.Fatal("ordinary workflow metadata was incorrectly reserved")
	}
}

func TestRemoteCodingRuntimeStartEndpointRejectsCallerChosenTargetAndGenericMetadata(t *testing.T) {
	const tokenSecret = "test-token-secret-0123456789012345"
	capture := &srvCaptureExecutor{}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: tokenSecret}, agentservice.NewMemoryStore(), capture)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	p := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), p, corelib.AppConfig{MaclawLLMUrl: "https://llm.example.test/v1", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model", SSHHosts: []corelib.SSHHostEntry{{Label: "build", Host: "build.example.test", User: "deploy", HostKeyFingerprint: "SHA256:fixed-pin"}}}); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(context.Background(), p, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CreateSession(context.Background(), p, inst.ID, agentservice.CreateSessionInput{Title: "Coding"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := agentservice.NewTokenManager(tokenSecret, time.Hour).Issue(p)
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	defer server.Close()
	path := "/api/v1/instances/" + inst.ID + "/sessions/" + session.ID

	request := httptest.NewRequest(http.MethodPost, path+"/messages", strings.NewReader(`{"content":"implement","metadata":{"coding_runtime_mode":"remote_workflow","coding_runtime_remote_host":"attacker.example.test"}}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || capture.calls != 0 {
		t.Fatalf("generic endpoint status=%d calls=%d body=%s", response.Code, capture.calls, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", strings.NewReader(`{"content":"implement","metadata":{"coding_runtime_mode":"remote_workflow"}}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || capture.calls != 0 {
		t.Fatalf("send endpoint status=%d calls=%d body=%s", response.Code, capture.calls, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, path+"/coding-runtime/remote", strings.NewReader(`{"content":"implement","workflow_id":"workflow-1","phase_id":"implementation","ssh_host_label":"build","work_dir":"/srv/app"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || capture.calls != 1 {
		t.Fatalf("runtime endpoint status=%d calls=%d body=%s", response.Code, capture.calls, response.Body.String())
	}
	meta := capture.request.Message.Metadata
	if meta["coding_runtime_mode"] != "remote_workflow" || meta["coding_runtime_remote_host"] != "build.example.test" || meta["coding_runtime_remote_host_key_fingerprint"] != "SHA256:fixed-pin" || meta["mutation_scope"] != "project" {
		t.Fatalf("runtime metadata was not resolved from configured target: %#v", meta)
	}
}

type srvCaptureExecutor struct {
	calls   int
	request agentservice.ExecuteRequest
}

func (e *srvCaptureExecutor) Execute(_ context.Context, request agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error) {
	e.calls++
	e.request = request
	return &agentservice.ExecuteResult{Content: "ok", OutputType: "text/plain"}, nil
}
