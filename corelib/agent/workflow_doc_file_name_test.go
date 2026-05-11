package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowDocWritePathUsesStableASCIIName(t *testing.T) {
	got := workflowDocWritePath(filepath.Join("docs", "需求文档", "需求文档.md"), map[string]interface{}{
		"phase_id": "requirements",
	})
	want := filepath.Join("docs", "01-requirements.md")
	if got != want {
		t.Fatalf("workflowDocWritePath() = %q, want %q", got, want)
	}
}

func TestWorkflowDocPhaseFromMetadataUsesEnum(t *testing.T) {
	if got := workflowDocPhaseFromMetadata("tech_design", "requirements"); got != workflowDocPhaseDesign {
		t.Fatalf("workflowDocPhaseFromMetadata phase wins = %q, want %q", got, workflowDocPhaseDesign)
	}
	if got := workflowDocPhaseFromMetadata("", "task_plan"); got != workflowDocPhaseTasks {
		t.Fatalf("workflowDocPhaseFromMetadata doc type = %q, want %q", got, workflowDocPhaseTasks)
	}
	if got := workflowDocPhaseFromMetadata("需求文档", ""); got != workflowDocPhaseUnknown {
		t.Fatalf("workflowDocPhaseFromMetadata localized string = %q, want unknown", got)
	}
}

func TestWorkflowDocWritePathDocTypeTaskPlan(t *testing.T) {
	got := workflowDocWritePath(filepath.Join("docs", "任务列表.md"), map[string]interface{}{
		"doc_type": "task_plan",
	})
	want := filepath.Join("docs", "03-task-breakdown.md")
	if got != want {
		t.Fatalf("workflowDocWritePath() = %q, want %q", got, want)
	}
}

func TestWorkflowDocDeliveryFileNameUsesStableASCIIName(t *testing.T) {
	got := workflowDocDeliveryFileNameWithFallbackExt("任务列表.文档", map[string]interface{}{
		"doc_type": "task_plan",
	}, ".pdf")
	if got != "03-task-breakdown.pdf" {
		t.Fatalf("workflowDocDeliveryFileNameWithFallbackExt() = %q, want 03-task-breakdown.pdf", got)
	}
}

func TestToolWriteFileNormalizesWorkflowDocumentPath(t *testing.T) {
	t.Chdir(t.TempDir())

	result := ToolWriteFile(map[string]interface{}{
		"path":     filepath.Join("docs", "需求文档", "需求文档.md"),
		"content":  "# Requirements",
		"phase_id": "requirements",
	})
	if strings.Contains(result, "需求文档.md") {
		t.Fatalf("ToolWriteFile result should use stable ASCII path, got %q", result)
	}
	if _, err := os.Stat(filepath.Join("docs", "01-requirements.md")); err != nil {
		t.Fatalf("expected normalized workflow document file: %v", err)
	}
	if _, err := os.Stat(filepath.Join("docs", "需求文档", "需求文档.md")); !os.IsNotExist(err) {
		t.Fatalf("localized workflow path should not be written, stat err=%v", err)
	}
}

func TestToolSendFileNormalizesWorkflowDisplayName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actual.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := ToolSendFile(map[string]interface{}{
		"path":      path,
		"file_name": "任务列表.文档",
		"doc_type":  "task_plan",
	})
	if !strings.HasPrefix(result, "[file_base64|03-task-breakdown.pdf|") {
		t.Fatalf("ToolSendFile result = %q, want stable ASCII display name", result)
	}
}

func TestToolSendFileForwardIMIncludesWorkflowDeliveryMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actual.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := ToolSendFile(map[string]interface{}{
		"path":          path,
		"file_name":     "需求文档.pdf",
		"phase_id":      "requirements",
		"forward_to_im": true,
	})
	if !strings.HasPrefix(result, "[file_base64|01-requirements.pdf|application/pdf|im|msg64:") {
		t.Fatalf("ToolSendFile result = %q, want stable ASCII name and message metadata", result)
	}
	flagStart := strings.Index(result, "|msg64:")
	if flagStart < 0 {
		t.Fatalf("ToolSendFile result missing msg64 flag: %q", result)
	}
	flagStart += len("|msg64:")
	flagEnd := strings.Index(result[flagStart:], "]")
	if flagEnd < 0 {
		t.Fatalf("ToolSendFile result missing payload close bracket: %q", result)
	}
	encoded := result[flagStart : flagStart+flagEnd]
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode msg64: %v", err)
	}
	if !strings.Contains(string(decoded), "需求文档") {
		t.Fatalf("decoded message = %q, want requirements delivery prompt", string(decoded))
	}
}
