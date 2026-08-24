package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// fakeMobileSemanticLLM emulates the Hub official chat endpoint for the mobile
// core agent. Requests carrying the fixed intent-tree system prompt are
// classification calls (no tools, no inventory) and answered with the canned
// intent JSON; every other request is an agent-loop call whose tool surface is
// captured for assertions. All responses use OpenAI SSE because both the
// classifier and the agent loop request streaming.
type fakeMobileSemanticLLM struct {
	intentJSON string

	mu        sync.Mutex
	loopTools [][]string
}

func (f *fakeMobileSemanticLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.Contains(string(body), "You are an intent classifier") {
		writeFakeMobileChatSSE(w, f.intentJSON)
		return
	}
	f.mu.Lock()
	f.loopTools = append(f.loopTools, fakeMobileChatToolNames(body))
	f.mu.Unlock()
	writeFakeMobileChatSSE(w, "done")
}

func (f *fakeMobileSemanticLLM) lastLoopTools() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.loopTools) == 0 {
		return nil
	}
	return append([]string(nil), f.loopTools[len(f.loopTools)-1]...)
}

func fakeMobileChatToolNames(body []byte) []string {
	var payload struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	names := make([]string, 0, len(payload.Tools))
	for _, tool := range payload.Tools {
		if name := strings.TrimSpace(tool.Function.Name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func writeFakeMobileChatSSE(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(delta string, finish string) string {
		payload := map[string]any{
			"id": "fake-mobile-llm", "object": "chat.completion.chunk", "created": 1, "model": "auto",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": finish,
				"delta": map[string]any{"role": "assistant", "content": delta},
			}},
		}
		data, _ := json.Marshal(payload)
		return string(data)
	}
	fmt.Fprintf(w, "data: %s\n\n", chunk(content, ""))
	fmt.Fprintf(w, "data: %s\n\n", chunk("", "stop"))
	io.WriteString(w, "data: [DONE]\n\n")
}

func hasToolNamePrefix(names []string, prefixes ...string) bool {
	for _, name := range names {
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}

// installMobileLookupSkill writes the minimal reviewed Skill used by the
// semantic-routing tests into the principal's skill inventory.
func installMobileLookupSkill(t *testing.T, svc *agentservice.Service, p agentservice.Principal) {
	t.Helper()
	skillDir := filepath.Join(svc.UserSkillsRoot(p.TenantID, p.UserID), "lookup")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id: acme.lookup\nname: lookup\nversion: v1\ndescription: untrusted description\nsteps:\n  - action: message\n    content: ok\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func publishMobileLookupSkillContract(t *testing.T, p agentservice.Principal) {
	t.Helper()
	svc, publisher, err := mobileDynamicCapabilityContractPublisher()
	if err != nil {
		t.Fatalf("publisher: %v", err)
	}
	// The publisher only binds contracts to principals known to the Service.
	if err := svc.EnsurePrincipal(context.Background(), p, p.UserID+"@example.com", p.UserID); err != nil {
		t.Fatalf("ensure principal: %v", err)
	}
	body := map[string]any{
		"provisions": []any{map[string]any{"capability": "information.lookup", "qualifiers": map[string]string{"scope": "reference"}, "quality": 1}},
		"effects":    []string{"read_only"},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var in mobileDynamicCapabilityPublicationRequest
	if err := json.Unmarshal(data, &in); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishObservedSkill(context.Background(), p, "acme.lookup", mobileDynamicCapabilityPublicationContract(in)); err != nil {
		t.Fatalf("publish observed skill contract: %v", err)
	}
}

func runMobileSemanticAgentTurn(t *testing.T, tenantID, userID string, llm *fakeMobileSemanticLLM, userText string) string {
	t.Helper()
	return runMobileSemanticAgentTurnWithAttachments(t, tenantID, userID, llm, userText, nil)
}

func runMobileSemanticAgentTurnWithAttachments(t *testing.T, tenantID, userID string, llm *fakeMobileSemanticLLM, userText string, attachments []agent.MessageAttachment) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/agent", nil)
	principal := &auth.ViewerPrincipal{TenantID: tenantID, UserID: userID, Email: userID + "@example.com"}
	messages := []map[string]string{{"role": "user", "content": userText}}
	answer, _, err := mobileRunCoreAgent(context.Background(), req, principal, llm, mobileLlmAuthorizationRecord{}, false, messages, nil, attachments)
	if err != nil {
		t.Fatalf("mobileRunCoreAgent: %v", err)
	}
	return answer
}

func TestMobileCoreAgentSemanticRoutingBootstrap(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	if _, _, err := mobileEnsureCoreAgent(); err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	svc, publisher, err := mobileDynamicCapabilityContractPublisher()
	if err != nil {
		t.Fatalf("mobileDynamicCapabilityContractPublisher: %v", err)
	}
	if svc == nil || publisher == nil {
		t.Fatal("expected configured service and contract publisher")
	}
	mobileDynamicRoutingMu.RLock()
	worker := mobileDynamicEffectWorker
	mobileDynamicRoutingMu.RUnlock()
	if worker == nil {
		t.Fatal("expected dynamic effect receipt worker to be started")
	}
}

func TestMobileCoreAgentManagedRequestUsesGrantBoundSemanticSurface(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"search","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "search the web for the latest Go release notes"); answer != "done" {
		t.Fatalf("managed answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("managed request exposed no grant-bound semantic tools")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("managed request leaked legacy bridge tools: %v", tools)
	}
}

func TestMobileCoreAgentLookupContinuationStaysUnmanaged(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"search","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "search the web for the latest Go release notes"); answer != "done" {
		t.Fatalf("lookup answer = %q", answer)
	}
	if tools := llm.lastLoopTools(); len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("lookup turn must stay on the grant-bound surface: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded lookup must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentCurrentTimeUsesHostClockAndStaysUnmanagedOnContinue(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"current_time","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, "tenant-a", "user-1", llm, "what time is it"); answer != "done" {
		t.Fatalf("current_time answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("current_time must expose the host-owned clock adapter")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("current_time leaked legacy bridge tools: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, "tenant-a", "user-1", llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded current_time must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentKnowledgeReadUsesHostStoreAndStaysUnmanagedOnContinue(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"knowledge_read","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "search my knowledge base for this topic"); answer != "done" {
		t.Fatalf("knowledge_read answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("knowledge_read must expose the host-owned knowledge adapter")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("knowledge_read leaked legacy bridge tools: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded knowledge_read must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentAuditReadUsesHostReaderAndStaysUnmanagedOnContinue(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)
	if err := svc.RecordAuditEvent(context.Background(), agentservice.AuditEvent{
		TenantID: principalA.TenantID, UserID: principalA.UserID,
		ActorType: "user", Action: "message.posted", ResourceType: "message", ResourceID: "m1",
	}); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"audit_read","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "show the recent security audit log"); answer != "done" {
		t.Fatalf("audit_read answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("audit_read must expose the host-owned audit adapter")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("audit_read leaked legacy bridge tools: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded audit_read must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentWebFetchUsesHostFetcherAndStaysUnmanagedOnContinue(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"web_fetch","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "fetch the content of this URL"); answer != "done" {
		t.Fatalf("web_fetch answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("web_fetch must expose the host-owned fetch adapter")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("web_fetch leaked legacy bridge tools: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded web_fetch must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentFileReadUsesHostReaderAndStaysUnmanagedOnContinue(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"file_read","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "show me what is in the README file"); answer != "done" {
		t.Fatalf("file_read answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("file_read must expose the host-owned workspace reader")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("file_read leaked legacy bridge tools: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded file_read must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentFileWriteUsesHostWriterAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"file_write","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "save this text to a local file"); answer != "done" {
		t.Fatalf("file_write answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("file_write must expose the host-owned workspace writer")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("file_write leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if strings.Contains(name, "write_file") || strings.Contains(name, "edit_file") {
			t.Fatalf("file_write leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a write receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentKnowledgeWriteUsesHostIngesterAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"knowledge_write","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "save this note into the knowledge base for future retrieval"); answer != "done" {
		t.Fatalf("knowledge_write answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("knowledge_write must expose the host-owned knowledge ingester")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("knowledge_write leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if strings.Contains(name, "knowledge_save") || strings.Contains(name, "knowledge_import") {
			t.Fatalf("knowledge_write leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before an ingest receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentMemoryManageUsesHostStoreAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"memory_manage","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "remember that I prefer Chinese"); answer != "done" {
		t.Fatalf("memory_manage answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("memory_manage must expose the host-owned memory manager")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("memory_manage leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if name == "memory" || strings.Contains(name, "knowledge_save") || strings.Contains(name, "knowledge_search") {
			t.Fatalf("memory_manage leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a memory receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentTaskTrackUsesHostStoreAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"task_track","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "show my current todo list"); answer != "done" {
		t.Fatalf("task_track answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("task_track must expose the host-owned task tracker")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("task_track leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if name == "task" || strings.Contains(name, "delegate") || strings.Contains(name, "manage_schedule") {
			t.Fatalf("task_track leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a task receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentGoalManageUsesHostStoreAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"goal_manage","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "create a long-running goal to keep this documentation up to date"); answer != "done" {
		t.Fatalf("goal_manage answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("goal_manage must expose the host-owned goal manager")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("goal_manage leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if name == "goal" || name == "task" || strings.Contains(name, "delegate") {
			t.Fatalf("goal_manage leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a goal receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentTemplateManageUsesHostStoreAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"template_manage","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "create a session template that uses codex"); answer != "done" {
		t.Fatalf("template_manage answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("template_manage must expose the host-owned template manager")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("template_manage leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if name == "manage_template" || name == "launch_template" || name == "list_sessions" || name == "manage_config" {
			t.Fatalf("template_manage leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a template receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentScheduleAdministerUsesHostStoreAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"schedule_manage","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "list all scheduled tasks"); answer != "done" {
		t.Fatalf("schedule_manage answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("schedule_manage must expose the host-owned schedule administrator")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("schedule_manage leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if name == "manage_schedule" || name == "schedule_administer" || name == "task" {
			t.Fatalf("schedule_manage leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a schedule receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentKnowledgeAdminUsesHostStoreAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"knowledge_admin","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "disable this knowledge base source"); answer != "done" {
		t.Fatalf("knowledge_admin answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("knowledge_admin must expose the host-owned knowledge administrator")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("knowledge_admin leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if name == "knowledge_maintain" || name == "knowledge_disable_source" || name == "knowledge_save_text" || name == "knowledge_search" {
			t.Fatalf("knowledge_admin leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a knowledge admin receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentConfigManageUsesHostStoreAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"config_manage","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "raise the max iteration limit"); answer != "done" {
		t.Fatalf("config_manage answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("config_manage must expose the host-owned config manager")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("config_manage leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if name == "manage_config" || name == "switch_llm_provider" || name == "set_max_iterations" || name == "manage_user_model" {
			t.Fatalf("config_manage leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a config receipt must replay the pending mutation, got %v", tools)
	}
}

func TestMobileCoreAgentSessionManageInspectsWithoutDriveAndReplaysUntilReceipt(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"session_manage","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "list my running coding sessions"); answer != "done" {
		t.Fatalf("session_manage answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("session_manage must expose the host-owned session inspector")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("session_manage leaked legacy bridge tools: %v", tools)
	}
	for _, name := range tools {
		if name == "list_sessions" || name == "interrupt_session" || name == "send_input" || name == "kill_session" || name == "launch_template" {
			t.Fatalf("session_manage leaked GUI tool names: %v", tools)
		}
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if len(tools) == 0 || hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("continue before a session inspect receipt must replay the pending inspect, got %v", tools)
	}
}

func TestMobileCoreAgentGitInspectUsesHostInspectorAndStaysUnmanagedOnContinue(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"git_inspect","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "show me the current diff"); answer != "done" {
		t.Fatalf("git_inspect answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("git_inspect must expose the host-owned repo inspector")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("git_inspect leaked legacy bridge tools: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded git_inspect must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentDocumentReadFailsClosedWithoutAttachment(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"document_read","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "read the attached document"); answer != "done" {
		t.Fatalf("document_read answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("document_read without attachment leaked legacy bridge tools: %v", tools)
	}
	if hasToolNamePrefix(tools, "read_file", "office", "read_document") {
		t.Fatalf("document_read without attachment leaked path-taking tools: %v", tools)
	}
}

func TestMobileCoreAgentAudioTranscribeFailsClosedWithoutAttachment(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"audio_transcribe","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "transcribe this recording"); answer != "done" {
		t.Fatalf("audio_transcribe answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("audio_transcribe without attachment leaked legacy bridge tools: %v", tools)
	}
	if hasToolNamePrefix(tools, "asr", "audio_transcribe", "record_audio") {
		t.Fatalf("audio_transcribe without attachment leaked GUI asr tools: %v", tools)
	}
}

type fakeMobileSpeechTranscriber struct{}

func (fakeMobileSpeechTranscriber) TranscribeSpeech(context.Context, string, []byte) (string, error) {
	return "recognized speech", nil
}

func TestMobileCoreAgentAudioTranscribeUsesHostEngineAndAttachment(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	exec, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	exec.SetReviewedHostSpeechTranscriber(fakeMobileSpeechTranscriber{})
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	attachments := []agent.MessageAttachment{{
		Type: "audio", FileName: "clip.wav", MimeType: "audio/wav",
		Data: "UklGRgAAAABXQVZF",
	}}
	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"audio_transcribe","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurnWithAttachments(t, principalA.TenantID, principalA.UserID, llm, "transcribe this recording", attachments); answer != "done" {
		t.Fatalf("audio_transcribe answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("audio_transcribe with a trusted attachment and host engine must expose the host transcriber")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_", "asr", "record_audio") {
		t.Fatalf("audio_transcribe leaked legacy or GUI asr tools: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded audio_transcribe must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentAudioTranscribeUsesOwnedRecordingAttachment(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	exec, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	exec.SetReviewedHostSpeechTranscriber(fakeMobileSpeechTranscriber{})
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "recording.wav"), []byte("RIFF owned-meeting"), 0o600); err != nil {
		t.Fatal(err)
	}
	viewer := &auth.ViewerPrincipal{TenantID: principalA.TenantID, UserID: principalA.UserID}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items["rec-owned"] = mobileMeetingRecording{
		ID: "rec-owned", OwnerID: principalA.UserID, TenantID: principalA.TenantID,
		Dir: dir, ContentType: "audio/wav", Status: "uploaded",
	}
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, "rec-owned")
		mobileMeetingRecordings.Unlock()
	})
	attachments := mobileTrustedAudioAttachments(viewer, "rec-owned")
	if len(attachments) != 1 {
		t.Fatalf("owned recording must publish one attachment, got %#v", attachments)
	}

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"audio_transcribe","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurnWithAttachments(t, principalA.TenantID, principalA.UserID, llm, "transcribe this recording", attachments); answer != "done" {
		t.Fatalf("audio_transcribe answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("owned recording attachment must expose the host transcriber")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_", "asr", "record_audio") {
		t.Fatalf("owned recording leaked legacy or GUI asr tools: %v", tools)
	}
}

func TestMobileReviewedHostSpeechTranscriberUsesMeetingWorker(t *testing.T) {
	SetMeetingRecordingWorkers(nil, nil)
	if (mobileReviewedHostSpeechTranscriber{}).Ready() {
		t.Fatal("host engine must stay unready without a meeting worker")
	}
	SetMeetingRecordingWorkers(testMeetingTranscriber{}, nil)
	t.Cleanup(func() { SetMeetingRecordingWorkers(nil, nil) })
	if !mobileSpeechTranscribeAvailable() || !(mobileReviewedHostSpeechTranscriber{}).Ready() {
		t.Fatal("configured meeting worker must make the host engine available")
	}
	got, err := (mobileReviewedHostSpeechTranscriber{}).TranscribeSpeech(context.Background(), "audio/wav", []byte("RIFF....WAVE"))
	if err != nil || got != "Alice: approved the launch date." {
		t.Fatalf("transcribe=%q err=%v", got, err)
	}
}

func TestMobileCoreAgentDocumentReadUsesOwnedDraftAttachment(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	viewer := &auth.ViewerPrincipal{TenantID: principalA.TenantID, UserID: principalA.UserID}
	mobileDocuments.Lock()
	mobileDocuments.drafts["doc-owned"] = mobileDocumentDraftRecord{
		ID: "doc-owned", OwnerID: principalA.UserID, TenantID: principalA.TenantID,
		Title: "notes", SourceFilename: "notes.txt", SourceContentType: "text/plain",
		SourceBytes: []byte("hello trusted original"),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "doc-owned")
		mobileDocuments.Unlock()
	})
	attachments := mobileTrustedDocumentAttachments(viewer, "doc-owned")
	if len(attachments) != 1 {
		t.Fatalf("owned draft must publish one attachment, got %#v", attachments)
	}

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"document_read","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurnWithAttachments(t, principalA.TenantID, principalA.UserID, llm, "read the attached document", attachments); answer != "done" {
		t.Fatalf("document_read answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if len(tools) == 0 {
		t.Fatal("document_read with a trusted attachment must expose the host reader")
	}
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_", "read_file", "office") {
		t.Fatalf("document_read leaked legacy or path-taking tools: %v", tools)
	}

	llm.intentJSON = `{"top":[{"skill":"continuation","score":0.90}]}`
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "continue"); answer != "done" {
		t.Fatalf("continuation answer = %q", answer)
	}
	tools = llm.lastLoopTools()
	if !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("continue after succeeded document_read must not replay as a mutation; want legacy bridge, got %v", tools)
	}
}

func TestMobileCoreAgentUnmanagedRequestKeepsLegacyBridgeSurface(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	// Keep the full prompt profile so the pre-existing light-profile tool
	// allowlist does not hide the legacy bridge surface from this assertion.
	t.Setenv("MACLAW_PROMPT_PROFILE", "full")
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"non_coding","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalA.TenantID, principalA.UserID, llm, "explain what a closure is"); answer != "done" {
		t.Fatalf("unmanaged answer = %q", answer)
	}
	if tools := llm.lastLoopTools(); !hasToolNamePrefix(tools, "invoke_skill_") {
		t.Fatalf("unmanaged request lost the legacy skill bridge surface: %v", tools)
	}
}

func TestMobileCoreAgentSemanticRoutingCrossPrincipalIsolation(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	t.Setenv(mobileStatePathEnv, filepath.Join(t.TempDir(), "state.json"))
	t.Setenv(mobileBlobDirEnv, filepath.Join(t.TempDir(), "blobs"))
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	principalA := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	principalB := agentservice.Principal{TenantID: "tenant-a", UserID: "user-2"}
	installMobileLookupSkill(t, svc, principalA)
	publishMobileLookupSkillContract(t, principalA)

	if _, ok := svc.DynamicCapabilityContracts().ResolveSkillDynamicContract(context.Background(), principalB, "acme.lookup"); ok {
		t.Fatal("principal B resolved principal A's published contract")
	}

	llm := &fakeMobileSemanticLLM{intentJSON: `{"top":[{"skill":"search","score":0.95}]}`}
	if answer := runMobileSemanticAgentTurn(t, principalB.TenantID, principalB.UserID, llm, "search the web for the latest Go release notes"); answer != "done" {
		t.Fatalf("isolated answer = %q", answer)
	}
	tools := llm.lastLoopTools()
	if hasToolNamePrefix(tools, "invoke_skill_", "invoke_mcp_") {
		t.Fatalf("principal B managed request fell back to the legacy bridge surface: %v", tools)
	}
	if len(tools) != 0 {
		t.Fatalf("principal B managed request must fail closed without a published contract, got tools: %v", tools)
	}
}

func TestMobileDynamicCapabilityPublicationEndpoints(t *testing.T) {
	resetMobileCoreAgentForTest()
	t.Setenv("MACLAW_EMBEDDING_DISABLED", "1")
	ctx := newAdminRouterTestContext(t)
	// Registered after the router context so the mobile agent Service (and its
	// SQLite files) is closed before TempDir cleanup removes the data root.
	t.Cleanup(resetMobileCoreAgentForTest)
	globalToken := issueHubAdminToken(t, ctx.handler)
	tenantToken := issueTenantAdminToken(t, ctx.handler, globalToken, "acme", "acme-owner")

	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatalf("mobileEnsureCoreAgent: %v", err)
	}
	publicationPrincipal := agentservice.Principal{TenantID: "tenant-a", UserID: "user-1"}
	if err := svc.EnsurePrincipal(context.Background(), publicationPrincipal, "user-1@example.com", "user-1"); err != nil {
		t.Fatalf("ensure principal: %v", err)
	}
	installMobileLookupSkill(t, svc, publicationPrincipal)

	skillPath := "/api/admin/tenants/tenant-a/users/user-1/dynamic-capabilities/skills/acme.lookup"
	body := map[string]any{
		"provisions": []any{map[string]any{"capability": "information.lookup", "qualifiers": map[string]string{"scope": "reference"}, "quality": 1}},
		"effects":    []string{"read_only"},
	}

	if resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, skillPath, body, ""); resp.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d body=%s", resp.Code, resp.Body.String())
	}
	confirmed := map[string]any{"confirm": true}
	for k, v := range body {
		confirmed[k] = v
	}
	if resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, skillPath, confirmed, tenantToken); resp.Code != http.StatusForbidden {
		t.Fatalf("tenant admin (non-owner) status = %d body=%s", resp.Code, resp.Body.String())
	}
	if resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, skillPath, body, globalToken); resp.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed status = %d body=%s", resp.Code, resp.Body.String())
	}
	resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, skillPath, confirmed, globalToken)
	if resp.Code != http.StatusCreated {
		t.Fatalf("skill publication status = %d body=%s", resp.Code, resp.Body.String())
	}
	var published map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published["contract_digest"] == "" || published["observed_binding_digest"] == "" {
		t.Fatalf("publication exposed missing digest fields: %#v", published)
	}

	// Missing observed binding must fail closed.
	missingPath := "/api/admin/tenants/tenant-a/users/user-1/dynamic-capabilities/skills/missing"
	if resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, missingPath, confirmed, globalToken); resp.Code == http.StatusCreated {
		t.Fatalf("missing skill binding was published: %s", resp.Body.String())
	}
	mcpPath := "/api/admin/tenants/tenant-a/users/user-1/dynamic-capabilities/mcp/srv1/tool1"
	if resp := doHubAdminJSONRequest(t, ctx.handler, http.MethodPost, mcpPath, confirmed, globalToken); resp.Code == http.StatusCreated {
		t.Fatalf("missing MCP binding was published: %s", resp.Body.String())
	}

	audits, err := ctx.store.AdminAudit.List(context.Background(), store.AdminAuditLogFilter{Action: "admin.dynamic_capability.skill_published", Limit: 10})
	if err != nil {
		t.Fatalf("list admin audit: %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("expected exactly one skill publication audit entry, got %#v", audits)
	}
}
