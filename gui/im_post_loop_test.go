package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkflowPhaseDocTextPrefersWrittenDocumentOverLongBuffer(t *testing.T) {
	dir := t.TempDir()
	docPath := filepath.Join(dir, "outline.md")
	doc := "# 内容大纲\n\n- 第一部分：背景\n- 第二部分：方案"
	if err := os.WriteFile(docPath, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := &LoopContext{WorkflowWrittenFiles: []string{docPath}}
	ctx.WorkflowDocBuffer.WriteString(strings.Repeat("我先检查目录，然后尝试生成文档。\n", 40))
	resp := &IMAgentResponse{Text: "最后一轮响应"}

	got, source := resolveWorkflowPhaseDocText(ctx, resp)
	if source != "written_files" {
		t.Fatalf("source = %q, want written_files", source)
	}
	if got != doc {
		t.Fatalf("doc = %q, want %q", got, doc)
	}
	if strings.Contains(got, "我先检查目录") {
		t.Fatalf("process narration leaked into document: %q", got)
	}
}

func TestResolveWorkflowPhaseDocTextFallsBackToBufferWithoutWrittenDocument(t *testing.T) {
	ctx := &LoopContext{}
	ctx.WorkflowDocBuffer.WriteString("# 逐页脚本\n\n- 第 1 页：开场")
	resp := &IMAgentResponse{Text: "fallback response"}

	got, source := resolveWorkflowPhaseDocText(ctx, resp)
	if source != "buffer" {
		t.Fatalf("source = %q, want buffer", source)
	}
	if got != "# 逐页脚本\n\n- 第 1 页：开场" {
		t.Fatalf("doc = %q", got)
	}
}
