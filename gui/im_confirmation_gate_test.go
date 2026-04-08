package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: "D:/work/project"}}
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
	if resp.Confirmation.TaskType != "coding" {
		t.Fatalf("expected coding confirmation, got %+v", resp.Confirmation)
	}
	if len(resp.Confirmation.TargetPaths) != 1 || resp.Confirmation.TargetPaths[0] != "D:/work/project" {
		t.Fatalf("unexpected target paths: %#v", resp.Confirmation.TargetPaths)
	}
	if got := h.confirmationStore.get("u1"); got == nil {
		t.Fatal("expected pending confirmation to be stored")
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
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: "D:/work/project"}}
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
	if got := classifyTaskIntent("生成宣传PPT"); got.Intent != intentNonCoding {
		t.Fatalf("expected presentation task to classify as non-coding, got %+v", got)
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
	cfg.MaclawLLMProviders = []MaclawLLMProvider{{
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
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: "D:/work/project"}}
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
	cfg.MaclawLLMProviders = []MaclawLLMProvider{{
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
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: "D:/work/project"}}
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
	h.confirmationStore.set(&pendingConfirmation{
		ID:           "c1",
		UserID:       "u1",
		OriginalText: "帮我修复 bug",
		ResumeText:   "帮我修复 bug\n\n用户补充/修正：目录改成 D:/fixed/project",
		Summary:      "summary",
		TaskType:     "coding",
		Status:       "pending",
		CreatedAt:    testNow(),
		UpdatedAt:    testNow(),
	})

	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{
		UserID:   "u1",
		Platform: "desktop",
		Text:     "确认，开始吧",
	}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error == "" {
		t.Fatalf("expected execution to continue into llm/config path, got %+v", resp)
	}
	if resp.Confirmation != nil {
		t.Fatalf("expected confirmation to be cleared after approval, got %+v", resp.Confirmation)
	}
	if got := h.confirmationStore.get("u1"); got != nil {
		t.Fatalf("expected pending confirmation to be cleared, got %+v", got)
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
		Text:     "取消这个任务",
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
