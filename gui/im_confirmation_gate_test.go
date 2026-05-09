package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestHandleIMMessageWithProgressAndStream_ReturnsConfirmationBeforeExecution(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.MaclawLLMUrl = "http://example.com"
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	cfg.UIMode = "pro"
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: "D:/work/project"}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     "fix the login bug in this project and update the code",
	}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Confirmation == nil {
		t.Fatalf("expected confirmation payload, got %+v", resp)
	}
	if resp.Confirmation.TaskType == "" {
		t.Fatalf("expected confirmation task type, got %+v", resp.Confirmation)
	}
	if len(resp.Confirmation.TargetPaths) != 1 || resp.Confirmation.TargetPaths[0] != corelib.EffectiveWorkspaceDir() {
		t.Fatalf("unexpected target paths: %#v (expected %s)", resp.Confirmation.TargetPaths, corelib.EffectiveWorkspaceDir())
	}
	if got := h.confirmationStore.get("u1"); got == nil {
		t.Fatal("expected pending confirmation to be stored")
	}
	if len(resp.Actions) < 2 || resp.Actions[0].Command != buildConfirmationActionCommand("confirm", resp.Confirmation.ID) {
		t.Fatalf("expected structured confirmation command, got %+v", resp.Actions)
	}
}

func TestHandleIMMessageWithProgressAndStream_PresentationTaskSkipsCodingConfirmation(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"intent\":\"non_coding\",\"confidence\":0.96,\"reason\":\"presentation task\",\"evidence\":[\"PPT\"]}"}}]}`)
	}))
	defer server.Close()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           server.URL,
		Model:         "test-model",
		Protocol:      "openai",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.UIMode = "pro"
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: "D:/work/project"}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     "鐢熸垚瀹ｄ紶PPT",
	}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Confirmation != nil {
		t.Fatalf("expected presentation task to skip coding confirmation, got %+v", resp.Confirmation)
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("expected no pending confirmation for presentation task, got %+v", got)
	}
	// Without UIC, route absence is unknown and should fall through to the
	// ordinary agent path instead of pre-execution confirmation.
	if got := classifyTaskIntent("鐢熸垚瀹ｄ紶PPT"); got.Intent != intentUnknown {
		t.Fatalf("expected presentation task to classify as unknown without UIC, got %+v", got)
	}
}

func TestHandleIMMessageWithProgressAndStream_ScreenshotTaskUsesLLMAndSkipsConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"intent\":\"non_coding\",\"confidence\":0.96,\"reason\":\"鐢ㄦ埛鍦ㄨ鍔╂墜鐞嗚В鎴浘骞舵暣鐞嗗浼犳潗鏂欙紝涓嶆秹鍙婁唬鐮佹垨鏈嶅姟鍣╘",\"evidence\":[\"鎴浘鍒嗘瀽\",\"瀹ｄ紶PPT\"]}"}}]}`)
	}))
	defer server.Close()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           server.URL,
		Model:         "test-model",
		Protocol:      "openai",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.UIMode = "pro"
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: "D:/work/project"}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     "鎼滅储椹卞姩寮€鍙戠綉椹媷鐨勮祫鏂欙紝浣跨敤skill鐢熸垚绮剧編鐨勫浼爌pt",
		Attachments: []MessageAttachment{
			{Type: "image", FileName: "screen.png", MimeType: "image/png"},
		},
	}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Confirmation != nil {
		t.Fatalf("expected screenshot task to skip confirmation, got %+v", resp.Confirmation)
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("expected no pending confirmation, got %+v", got)
	}
}

func TestHandleIMMessageWithProgressAndStream_LLMFailureFallsThroughToAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           server.URL,
		Model:         "test-model",
		Protocol:      "openai",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.UIMode = "pro"
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: "D:/work/project"}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     "fix the login bug and edit code",
	}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Confirmation != nil {
		t.Fatalf("expected LLM failure to fall through to agent instead of confirmation, got %+v", resp.Confirmation)
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("expected no pending confirmation after route-classifier failure, got %+v", got)
	}
}

func TestHandleIMMessageWithProgressAndStream_ApproveConfirmationResumesOriginalTask(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.MaclawLLMUrl = "http://example.com"
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	pending := &pendingConfirmation{
		ID:           "c1",
		UserID:       "u1",
		OriginalText: "甯垜淇 bug",
		ResumeText:   "甯垜淇 bug\n\n鐢ㄦ埛琛ュ厖/淇锛氱洰褰曟敼鎴?D:/fixed/project",
		Summary:      "summary",
		TaskType:     "coding",
		Status:       "pending",
		CreatedAt:    testNow(),
		UpdatedAt:    testNow(),
	}
	h.confirmationStore.set(pending)

	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     buildConfirmationActionCommand("confirm", "c1"),
		UIAction: true,
	}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	approvedText := confirmationApprovedText(pending)
	if !strings.Contains(approvedText, "[鎵ц涓婁笅鏂嘳") {
		t.Fatalf("expected approved text to carry execution context, got %q", approvedText)
	}
	if !strings.Contains(approvedText, "鐢ㄦ埛宸茬‘璁ゅ綋鍓嶆柟妗堬紝璇风洿鎺ュ紑濮嬫墽琛岋紝涓嶈鍐嶆璇锋眰纭") {
		t.Fatalf("expected approved text to include confirmation directive, got %q", approvedText)
	}
	if resp.Error == "" {
		t.Fatalf("expected execution to continue into llm/config path, got %+v", resp)
	}
	if !resp.ConfirmedResume {
		t.Fatalf("expected confirmed resume marker, got %+v", resp)
	}
	if resp.Confirmation != nil {
		t.Fatalf("expected confirmation to be cleared after approval, got %+v", resp.Confirmation)
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("expected pending confirmation to be cleared, got %+v", got)
	}
}

func TestNormalizeConfirmationIntentRequiresExactCategory(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{input: "confirm", want: "confirm"},
		{input: " confirm. ", want: "confirm"},
		{input: "cancel", want: "cancel"},
		{input: "modify", want: "modify"},
		{input: "not confirm", want: ""},
		{input: "confirm or modify", want: ""},
	}
	for _, tc := range cases {
		if got := normalizeConfirmationIntent(tc.input); got != tc.want {
			t.Fatalf("normalizeConfirmationIntent(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestClassifyConfirmationIntent_UsesContextForTypedApproval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"confirm"}}]}`)
	}))
	defer server.Close()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           server.URL,
		Model:         "test-model",
		Protocol:      "openai",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	pending := &pendingConfirmation{ID: "c1", UserID: "u1", Summary: "summary", TaskType: "coding", Status: "pending"}
	if got := h.classifyConfirmationIntent("u1", "go ahead", pending); got != "confirm" {
		t.Fatalf("expected semantic typed approval to classify as confirm, got %q", got)
	}
}

func TestHandleIMMessageWithProgressAndStream_RevisionKeepsPendingConfirmation(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.MaclawLLMUrl = "http://example.com"
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.confirmationStore.set(&pendingConfirmation{
		ID:           "c1",
		UserID:       "u1",
		OriginalText: "甯垜淇 bug",
		ResumeText:   "甯垜淇 bug",
		Summary:      "鍘熷鎬荤粨",
		TaskType:     "coding",
		Status:       "pending",
		CreatedAt:    testNow(),
		UpdatedAt:    testNow(),
	})

	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     "鐩綍涓嶅锛屽簲璇ュ湪 D:/new/project",
	}, nil, nil, nil, nil)
	if resp == nil || resp.Confirmation == nil {
		t.Fatalf("expected updated confirmation response, got %+v", resp)
	}
	stored := h.confirmationStore.get("u1")
	if stored == nil {
		t.Fatal("expected pending confirmation to remain stored")
	}
	if stored.ResumeText == "甯垜淇 bug" {
		t.Fatalf("expected resume text to include revision, got %+v", stored)
	}
}

func TestHandleIMMessageWithProgressAndStream_CancelPendingConfirmation(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.MaclawLLMUrl = "http://example.com"
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.confirmationStore.set(&pendingConfirmation{
		ID:           "c1",
		UserID:       "u1",
		OriginalText: "甯垜淇 bug",
		ResumeText:   "甯垜淇 bug",
		Summary:      "summary",
		TaskType:     "coding",
		Status:       "pending",
		CreatedAt:    testNow(),
		UpdatedAt:    testNow(),
	})

	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     buildConfirmationActionCommand("cancel", "c1"),
		UIAction: true,
	}, nil, nil, nil, nil)
	if resp == nil || resp.Text == "" {
		t.Fatalf("expected cancel response, got %+v", resp)
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("expected pending confirmation to be cleared, got %+v", got)
	}
}

func testNow() time.Time {
	return time.Now()
}
