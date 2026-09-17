package guiapp

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// credentialGateTestHandler builds a minimal handler with a registered
// knowledge_save_text tool whose handler increments called.
func credentialGateTestHandler(t *testing.T, called *int32) *IMMessageHandler {
	t.Helper()
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:    "knowledge_save_text",
		Handler: func(args map[string]interface{}) string { atomic.AddInt32(called, 1); return "saved" },
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	app := &App{testHomeDir: t.TempDir()}
	return &IMMessageHandler{registry: registry, app: app, confirmationStore: newAIConfirmationStore("")}
}

func waitForCredentialCard(t *testing.T, h *IMMessageHandler, userID string) *pendingConfirmation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if card := h.confirmationStore.get(userID); card != nil {
			return card
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("credential card was not pushed to the confirmation store")
	return nil
}

const credentialGateSecretArgs = `{"text":"驱网服务器 www.driverdevelop.com root sunion123"}`

func TestCredentialGateToolMatcher(t *testing.T) {
	for _, name := range []string{
		"knowledge_save_text", "knowledge_save_url", "knowledge_save_urls",
		"knowledge_import_directory", "knowledge_import_files", "knowledge_import_snapshot",
		"semantic_ingest_trusted_knowledge",
	} {
		if !credentialGatedKnowledgeWriteTool(name) {
			t.Errorf("credentialGatedKnowledgeWriteTool(%q) = false, want true", name)
		}
	}
	for _, name := range []string{
		"knowledge_search", "knowledge_import_status", "knowledge_stats", "memory", "write_file", "",
	} {
		if credentialGatedKnowledgeWriteTool(name) {
			t.Errorf("credentialGatedKnowledgeWriteTool(%q) = true, want false", name)
		}
	}
}

// The core fence flow: a secret payload suspends the tool call, the card is
// stored redacted, a matching button command approves, and only then the
// write runs.
func TestCredentialGateSecretPayloadSuspendsUntilApproved(t *testing.T) {
	var called int32
	h := credentialGateTestHandler(t, &called)

	done := make(chan toolExecutionResult, 1)
	go func() {
		done <- h.executeToolDetailedWithRuntimeContext(context.Background(), "u1", false, "", "knowledge_save_text", credentialGateSecretArgs, "", nil)
	}()

	card := waitForCredentialCard(t, h, "u1")
	if card.TaskType != credentialCardTaskType {
		t.Fatalf("card task type = %q, want %q", card.TaskType, credentialCardTaskType)
	}
	if strings.Contains(card.Summary, "sunion123") {
		t.Fatalf("card summary must be redacted, got %q", card.Summary)
	}
	if !strings.Contains(card.Summary, "[REDACTED]") {
		t.Fatalf("card summary should show a redacted preview, got %q", card.Summary)
	}
	if card.ResumeText != "" || card.OriginalText != "" {
		t.Fatalf("credential card must not be resumable, got original=%q resume=%q", card.OriginalText, card.ResumeText)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("write handler must not run before approval")
	}

	cmd := buildConfirmationActionCommand(confirmationActionConfirm, card.ID)
	trimmed := cmd
	res := h.handlePendingExecutionConfirmation(&IMUserMessage{UserID: "u1", Platform: "desktop", Text: cmd, UIAction: true}, &trimmed)
	if !res.Handled || res.ConfirmedResume {
		t.Fatalf("credential approval must be handled without resume, got %+v", res)
	}
	if h.confirmationStore.get("u1") != nil {
		t.Fatal("store must be cleared after credential approval")
	}

	result := <-done
	if result.Outcome != toolOutcomeSucceeded || atomic.LoadInt32(&called) != 1 {
		t.Fatalf("after approval the write must run: result=%+v called=%d", result, called)
	}
}

func TestCredentialGateCancelCommandRejects(t *testing.T) {
	var called int32
	h := credentialGateTestHandler(t, &called)

	done := make(chan toolExecutionResult, 1)
	go func() {
		done <- h.executeToolDetailedWithRuntimeContext(context.Background(), "u1", false, "", "knowledge_save_text", credentialGateSecretArgs, "", nil)
	}()

	card := waitForCredentialCard(t, h, "u1")
	cmd := buildConfirmationActionCommand(confirmationActionCancel, card.ID)
	trimmed := cmd
	res := h.handlePendingExecutionConfirmation(&IMUserMessage{UserID: "u1", Platform: "desktop", Text: cmd, UIAction: true}, &trimmed)
	if !res.Handled {
		t.Fatalf("cancel command must be handled, got %+v", res)
	}
	if h.confirmationStore.get("u1") != nil {
		t.Fatal("store must be cleared after credential cancel")
	}

	result := <-done
	if result.Outcome != toolOutcomeFailed || atomic.LoadInt32(&called) != 0 {
		t.Fatalf("after cancel the write must not run: result=%+v called=%d", result, called)
	}
}

// Free text ("好的") must NOT approve a credential card — the card is voided
// as rejected and the user is told to resend.
func TestCredentialGateFreeTextVoidsCard(t *testing.T) {
	var called int32
	h := credentialGateTestHandler(t, &called)

	done := make(chan toolExecutionResult, 1)
	go func() {
		done <- h.executeToolDetailedWithRuntimeContext(context.Background(), "u1", false, "", "knowledge_save_text", credentialGateSecretArgs, "", nil)
	}()

	waitForCredentialCard(t, h, "u1")
	trimmed := "好的"
	res := h.handlePendingExecutionConfirmation(&IMUserMessage{UserID: "u1", Platform: "desktop", Text: trimmed}, &trimmed)
	if !res.Handled || res.Response == nil {
		t.Fatalf("free text while credential card pending must be handled, got %+v", res)
	}
	if res.ConfirmedResume {
		t.Fatal("free text must never resume a credential card")
	}
	if h.confirmationStore.get("u1") != nil {
		t.Fatal("card must be voided after free text")
	}

	result := <-done
	if result.Outcome != toolOutcomeFailed || atomic.LoadInt32(&called) != 0 {
		t.Fatalf("voided card must reject the write: result=%+v called=%d", result, called)
	}
}

// Loop cancellation must pierce the fence: the tool call returns an error,
// the pending card is voided, and clicking the voided card says expired.
func TestCredentialGateLoopCancelPiercesFence(t *testing.T) {
	var called int32
	h := credentialGateTestHandler(t, &called)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan toolExecutionResult, 1)
	go func() {
		done <- h.executeToolDetailedWithRuntimeContext(ctx, "u1", false, "", "knowledge_save_text", credentialGateSecretArgs, "", nil)
	}()

	card := waitForCredentialCard(t, h, "u1")
	cancel()

	result := <-done
	if result.Outcome != toolOutcomeFailed {
		t.Fatalf("cancelled fence must fail the tool call, got %+v", result)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("write handler must not run after cancel")
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("loop death must void the pending card, got %+v", got)
	}

	// Clicking the voided card must answer "expired", never resume.
	cmd := buildConfirmationActionCommand(confirmationActionConfirm, card.ID)
	trimmed := cmd
	res := h.handlePendingExecutionConfirmation(&IMUserMessage{UserID: "u1", Platform: "desktop", Text: cmd, UIAction: true}, &trimmed)
	if !res.Handled || res.ConfirmedResume {
		t.Fatalf("voided card click must be handled as expired, got %+v", res)
	}
}

func TestCredentialGateTimeoutRejects(t *testing.T) {
	old := credentialFenceTimeout
	credentialFenceTimeout = 60 * time.Millisecond
	t.Cleanup(func() { credentialFenceTimeout = old })

	var called int32
	h := credentialGateTestHandler(t, &called)

	done := make(chan toolExecutionResult, 1)
	go func() {
		done <- h.executeToolDetailedWithRuntimeContext(context.Background(), "u1", false, "", "knowledge_save_text", credentialGateSecretArgs, "", nil)
	}()

	waitForCredentialCard(t, h, "u1")
	result := <-done
	if result.Outcome != toolOutcomeFailed {
		t.Fatalf("timeout must fail the tool call, got %+v", result)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("write handler must not run on timeout")
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("timeout must void the pending card, got %+v", got)
	}
}

// Clean payloads take the original path with no card and no added blocking.
func TestCredentialGateCleanPayloadZeroInterference(t *testing.T) {
	var called int32
	h := credentialGateTestHandler(t, &called)

	result := h.executeToolDetailedWithRuntimeContext(context.Background(), "u1", false, "", "knowledge_save_text", `{"text":"普通的知识库笔记内容"}`, "", nil)
	if result.Outcome != toolOutcomeSucceeded || atomic.LoadInt32(&called) != 1 {
		t.Fatalf("clean payload must execute directly: %+v called=%d", result, called)
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("no card expected for a clean payload, got %+v", got)
	}
}

// Regression guard for the convergence isolation: a plan confirmation card
// must keep its free-text (LLM-classified) behavior — only credential cards
// void on free text.
func TestPlanConfirmationFreeTextStillClassifies(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}, confirmationStore: newAIConfirmationStore("")}
	h.confirmationStore.set(&pendingConfirmation{
		ID:           "plan-1",
		UserID:       "u1",
		OriginalText: "fix the login bug",
		ResumeText:   "fix the login bug",
		Summary:      "summary",
		TaskType:     "coding",
		Status:       confirmationStatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
	trimmed := "目录改成 D:/other/project"
	res := h.handlePendingExecutionConfirmation(&IMUserMessage{UserID: "u1", Platform: "desktop", Text: trimmed}, &trimmed)
	if !res.Handled || res.Response == nil || res.Response.Confirmation == nil {
		t.Fatalf("plan card free text must keep revision behavior, got %+v", res)
	}
	if got := h.confirmationStore.get("u1"); got == nil {
		t.Fatal("plan card must survive free text (revision path)")
	}
}

// The semantic ingest adapter bypasses the registry executor; its fence must
// behave identically (suspend → card → approve → proceed).
func TestCredentialGateSemanticIngestSuspendsUntilApproved(t *testing.T) {
	h := credentialGateTestHandler(t, new(int32))

	done := make(chan string, 1)
	go func() {
		proceed, rejection := h.credentialGateSemanticIngest(nil, "u1", "", "服务器 root sunion123", "", "")
		if !proceed {
			done <- rejection
			return
		}
		done <- ""
	}()

	card := waitForCredentialCard(t, h, "u1")
	cmd := buildConfirmationActionCommand(confirmationActionConfirm, card.ID)
	trimmed := cmd
	res := h.handlePendingExecutionConfirmation(&IMUserMessage{UserID: "u1", Platform: "desktop", Text: cmd, UIAction: true}, &trimmed)
	if !res.Handled {
		t.Fatalf("semantic ingest card approval must be handled, got %+v", res)
	}
	if rejection := <-done; rejection != "" {
		t.Fatalf("semantic ingest must proceed after approval, got rejection %q", rejection)
	}
}

// A pending plan confirmation already occupies the single-slot store: the
// credential gate must fail closed instead of overwriting it (overwriting
// would destroy the plan card's ResumeText and make the original task
// unresumable).
func TestCredentialGateOccupiedStoreFailsClosed(t *testing.T) {
	var called int32
	h := credentialGateTestHandler(t, &called)
	h.confirmationStore.set(&pendingConfirmation{
		ID:           "plan-1",
		UserID:       "u1",
		OriginalText: "fix the login bug",
		ResumeText:   "fix the login bug",
		Summary:      "summary",
		TaskType:     "coding",
		Status:       confirmationStatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	result := h.executeToolDetailedWithRuntimeContext(context.Background(), "u1", false, "", "knowledge_save_text", credentialGateSecretArgs, "", nil)
	if result.Outcome != toolOutcomeFailed {
		t.Fatalf("occupied store must fail closed, got %+v", result)
	}
	if result.Text == "" {
		t.Fatal("occupied-store rejection must carry an explanatory message")
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Fatal("write handler must not run when the store is occupied")
	}
	plan := h.confirmationStore.get("u1")
	if plan == nil || plan.ID != "plan-1" || plan.ResumeText != "fix the login bug" {
		t.Fatalf("plan card must survive untouched, got %+v", plan)
	}
}

// The inline-interrupt bypass is scoped to confirmation action commands only;
// task-switch internal commands and free text keep the pre-existing
// interrupt/queue path.
func TestShouldTryInlineInterruptBypassesOnlyConfirmationCommands(t *testing.T) {
	const userID = "u-inline-bypass"
	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	h.setSessionLoopCtx(userID, NewLoopContext("long task", 3, nil))
	h.interruptHandler.handler = h

	if h.shouldTryInlineInterrupt(IMUserMessage{UserID: userID, Text: "__confirm_execution__ c1"}) {
		t.Fatal("confirm action commands must bypass inline interrupt")
	}
	if h.shouldTryInlineInterrupt(IMUserMessage{UserID: userID, Text: "__cancel_execution__ c1"}) {
		t.Fatal("cancel action commands must bypass inline interrupt")
	}
	if !h.shouldTryInlineInterrupt(IMUserMessage{UserID: userID, Text: "__workflow_choice__ w1"}) {
		t.Fatal("workflow choice commands must keep the inline interrupt path")
	}
	if !h.shouldTryInlineInterrupt(IMUserMessage{UserID: userID, Text: "__start_new_task__"}) {
		t.Fatal("task-switch commands must keep the inline interrupt path")
	}
	if !h.shouldTryInlineInterrupt(IMUserMessage{UserID: userID, Text: "__resume_unfinished__ r1"}) {
		t.Fatal("resume commands must keep the inline interrupt path")
	}
	if !h.shouldTryInlineInterrupt(IMUserMessage{UserID: userID, Text: "ordinary free text"}) {
		t.Fatal("free text must keep the inline interrupt path while a loop is active")
	}
}

// The pending-binding bug: an internal command text must not be swallowed as
// a pending ask_user answer.
func TestConsumePendingAskUserAnswerSkipsInternalCommand(t *testing.T) {
	h := &IMMessageHandler{}
	h.pendingAskUser.Store("u1", &pendingAskUserState{Question: "选哪个？"})

	ctx, ws, ok := h.consumePendingAskUserAnswer("u1", "__confirm_execution__ card-1", nil)
	if ok || ctx != "" || ws != nil {
		t.Fatalf("internal command must not bind as ask_user answer: ctx=%q ws=%v ok=%v", ctx, ws, ok)
	}
	if _, stillPending := h.pendingAskUser.Load("u1"); stillPending {
		t.Fatal("internal command should discard the stale pending ask_user binding")
	}
}
