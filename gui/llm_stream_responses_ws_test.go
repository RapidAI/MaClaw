package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBuildResponsesWSHeadersDefaultsCodeGenClientName(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Key: "sk-test", AgentType: "openclaw"}
	headers := buildResponsesWSHeaders(cfg, "wss://codegen.qianxin-inc.cn/api/v1/responses")
	if got := headers.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
		t.Fatalf("%s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
	}
	if got := headers.Get("User-Agent"); got != "openclaw" {
		t.Fatalf("User-Agent = %q, want openclaw", got)
	}
}

func TestBuildResponsesWSHeadersPreservesCustomCodeGenClientName(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Key: "sk-test", AgentType: "custom-agent"}
	headers := buildResponsesWSHeaders(cfg, "wss://api.codegen.qianxin-inc.cn/api/v1/responses")
	if got := headers.Get(corelib.CodeGenClientNameHeader); got != "custom-agent" {
		t.Fatalf("%s = %q, want custom-agent", corelib.CodeGenClientNameHeader, got)
	}
}

func TestBuildResponsesWSHeadersSkipsNonCodeGenURL(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Key: "sk-test", AgentType: "custom-agent"}
	headers := buildResponsesWSHeaders(cfg, "wss://api.example.com/v1/responses")
	if got := headers.Get(corelib.CodeGenClientNameHeader); got != "" {
		t.Fatalf("non-CodeGen %s = %q, want empty", corelib.CodeGenClientNameHeader, got)
	}
}
