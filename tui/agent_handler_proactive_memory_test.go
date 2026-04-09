package main

import (
	"net/http"
	"os"
	"path/filepath"
	pathpkg "path/filepath"
	"strings"
	"testing"
	"time"

	corelibpkg "github.com/RapidAI/CodeClaw/corelib"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

func newTestTUIHandlerWithMemoryStore(t *testing.T) *TUIAgentHandler {
	t.Helper()
	tmpDir := t.TempDir()
	memPath := pathpkg.Join(tmpDir, "memories.json")
	ms, err := corememory.NewStore(memPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(ms.Stop)
	return &TUIAgentHandler{
		memoryStore:      ms,
		codingToolHealth: newCodingToolHealthCache(),
	}
}

func assertPromptContainsAll(t *testing.T, text string, parts []string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Errorf("missing %q", part)
		}
	}
}

func assertPromptContainsNone(t *testing.T, text string, parts []string) {
	t.Helper()
	for _, part := range parts {
		if strings.Contains(text, part) {
			t.Errorf("unexpectedly contained %q", part)
		}
	}
}

func TestTUIWriteFile_AllowsEmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	h := &TUIAgentHandler{}
	got := h.toolWriteFile(map[string]interface{}{"path": "empty.txt", "content": ""})
	if !strings.Contains(got, "已清空") {
		t.Fatalf("toolWriteFile() = %q, want clear message", got)
	}
	data, err := os.ReadFile(filepath.Join(tmpDir, "empty.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "" {
		t.Fatalf("file content = %q, want empty", string(data))
	}
}

func TestTUIWriteFile_RejectsMissingContentField(t *testing.T) {
	h := &TUIAgentHandler{}
	got := h.toolWriteFile(map[string]interface{}{"path": "empty.txt"})
	if got != "错误: 缺少 content 参数" {
		t.Fatalf("toolWriteFile() = %q, want missing content error", got)
	}
}

func TestTUIToolGeneratePDF_IsInternalOnly(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	h := &TUIAgentHandler{codingToolHealth: newCodingToolHealthCache()}
	if got := h.dispatchTool("generate_pdf", map[string]interface{}{"content": "# 标题\n\n正文"}); !strings.Contains(got, "未知工具") {
		t.Fatalf("generate_pdf should not be dispatchable, got: %s", got)
	}
}

func TestPDFContentValidation_RejectsOversizedParagraph(t *testing.T) {
	if err := swarm.ValidatePDFContent(strings.Repeat("段", 48*1024+1)); err == nil || !strings.Contains(err.Error(), "过长段落") {
		t.Fatalf("expected oversized paragraph error, got: %v", err)
	}
}

func TestPDFContentValidation_RejectsFilePayloadMarker(t *testing.T) {
	if err := swarm.ValidatePDFContent("# 报告\n\n[file_base64|x|application/pdf]AAAA"); err == nil || !strings.Contains(err.Error(), "文件载荷") {
		t.Fatalf("expected file payload error, got: %v", err)
	}
}

func TestNewTUIAgentHandler_UsesConfiguredTimeout(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("MACLAW_DATA_DIR", pathpkg.Join(tmpHome, ".maclaw"))

	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg := corelibpkg.AppConfig{
		MaclawLLMUrl:        "https://example.com/v1",
		MaclawLLMModel:      "glm-5.1",
		MaclawLLMProtocol:   "anthropic",
		MaclawLLMTimeoutSec: 540,
	}
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewTUIAgentHandler(nil)
	transport, ok := h.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", h.httpClient.Transport)
	}
	want := 540 * time.Second
	if transport.ResponseHeaderTimeout != want {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, want)
	}
}

func TestTUISystemPrompt_WithMemoryStore_ContainsProactiveInstruction(t *testing.T) {
	h := newTestTUIHandlerWithMemoryStore(t)
	prompt := h.buildSystemPromptWithFirstTurn("hello", true)
	assertPromptContainsAll(t, prompt, []string{
		corememory.BuildTUIProactiveMemoryPrompt(),
	})
}

func TestTUISystemPrompt_NonFirstTurn_NoProactiveInstruction(t *testing.T) {
	h := newTestTUIHandlerWithMemoryStore(t)
	prompt := h.buildSystemPromptWithFirstTurn("follow up", false)
	assertPromptContainsNone(t, prompt, []string{
		corememory.PromptSectionProactiveMemory,
		corememory.PromptActionSaveEquals,
		corememory.PromptProactiveAck,
	})
}

func TestTUISystemPrompt_HistoryBasedFirstTurnDetection(t *testing.T) {
	h := newTestTUIHandlerWithMemoryStore(t)
	followUpPrompt := h.buildSystemPromptWithHistory("follow up", []map[string]string{
		{"role": "user", "content": "hello"},
		{"role": "assistant", "content": "hi"},
	})
	assertPromptContainsNone(t, followUpPrompt, []string{corememory.PromptSectionProactiveMemory})

	clearedPrompt := h.buildSystemPromptWithHistory("fresh start", nil)
	assertPromptContainsAll(t, clearedPrompt, []string{
		corememory.BuildTUIProactiveMemoryPrompt(),
	})
}

func TestTUISystemPrompt_WithoutMemoryStore_NoProactiveInstruction(t *testing.T) {
	h := &TUIAgentHandler{codingToolHealth: newCodingToolHealthCache()}
	prompt := h.buildSystemPromptWithFirstTurn("hello", true)
	assertPromptContainsNone(t, prompt, []string{corememory.PromptSectionProactiveMemory})
}
