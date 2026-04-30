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
		Text:     "帮我修复这个项目里的登录 bug 并修改代码",
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
		Text:     "生成宣传PPT",
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
	// Without UIC, classifyTaskIntent returns ambiguous (conservative).
	// With UIC, it would return the correct semantic classification.
	if got := classifyTaskIntent("生成宣传PPT"); got.Intent != intentAmbiguous {
		t.Fatalf("expected presentation task to classify as ambiguous without UIC, got %+v", got)
	}
}

func TestHandleIMMessageWithProgressAndStream_ScreenshotTaskUsesLLMAndSkipsConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"intent\":\"non_coding\",\"confidence\":0.96,\"reason\":\"用户在让助手理解截图并整理宣传材料，不涉及代码或服务器\",\"evidence\":[\"截图分析\",\"宣传PPT\"]}"}}]}`)
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
		Text:     "搜索驱动开发网马勇的资料，使用skill生成精美的宣传ppt",
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

func TestHandleIMMessageWithProgressAndStream_FallbackToRulesOnLLMFailure(t *testing.T) {
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
		Text:     "帮我修复这个项目里的登录 bug 并修改代码",
	}, nil, nil, nil, nil)
	if resp == nil || resp.Confirmation == nil {
		t.Fatalf("expected fallback confirmation, got %+v", resp)
	}
	if resp.Confirmation.TaskType != "coding" {
		t.Fatalf("expected fallback to coding confirmation, got %+v", resp.Confirmation)
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
		OriginalText: "帮我修复 bug",
		ResumeText:   "帮我修复 bug\n\n用户补充/修正：目录改成 D:/fixed/project",
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
	if !strings.Contains(approvedText, "[执行上下文]") {
		t.Fatalf("expected approved text to carry execution context, got %q", approvedText)
	}
	if !strings.Contains(approvedText, "用户已确认当前方案，请直接开始执行，不要再次请求确认") {
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
		OriginalText: "帮我修复 bug",
		ResumeText:   "帮我修复 bug",
		Summary:      "原始总结",
		TaskType:     "coding",
		Status:       "pending",
		CreatedAt:    testNow(),
		UpdatedAt:    testNow(),
	})

	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     "目录不对，应该在 D:/new/project",
	}, nil, nil, nil, nil)
	if resp == nil || resp.Confirmation == nil {
		t.Fatalf("expected updated confirmation response, got %+v", resp)
	}
	stored := h.confirmationStore.get("u1")
	if stored == nil {
		t.Fatal("expected pending confirmation to remain stored")
	}
	if stored.ResumeText == "帮我修复 bug" {
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
		OriginalText: "帮我修复 bug",
		ResumeText:   "帮我修复 bug",
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
