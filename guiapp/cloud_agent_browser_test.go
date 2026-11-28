package guiapp

import (
	"strings"
	"testing"
)

func TestCloudAgentPreviewCacheNameFollowsContent(t *testing.T) {
	first := cloudAgentPreviewCacheName("docs/report.pdf", "report.pdf", []byte("%PDF-1"))
	second := cloudAgentPreviewCacheName("docs/report.pdf", "report.pdf", []byte("%PDF-2"))
	same := cloudAgentPreviewCacheName("docs/report.pdf", "report.pdf", []byte("%PDF-1"))
	other := cloudAgentPreviewCacheName("other/report.pdf", "report.pdf", []byte("%PDF-1"))
	if first == second {
		t.Fatalf("changed bytes reused %s", first)
	}
	if first != same {
		t.Fatalf("identical bytes changed name: %s vs %s", first, same)
	}
	if first == other {
		t.Fatalf("different paths collided: %s", first)
	}
	if strings.ContainsAny(first, `/\`) {
		t.Fatalf("name has a separator: %s", first)
	}
}

func TestMaterializeCloudAgentPreviewFileRejectsSourceFiles(t *testing.T) {
	app := &App{}
	id := "cag_" + strings.Repeat("a", 32)
	if _, err := app.MaterializeCloudAgentPreviewFile("cloud-agent:"+id, "main.cpp"); err == nil || !strings.Contains(err.Error(), "binary files cannot be previewed") {
		t.Fatalf("source file: %v", err)
	}
	if _, err := app.MaterializeCloudAgentPreviewFile("cloud-agent:"+id, "notes.doc"); err == nil || !strings.Contains(err.Error(), "binary files cannot be previewed") {
		t.Fatalf("legacy doc: %v", err)
	}
	if _, err := app.MaterializeCloudAgentPreviewFile(`D:\proj`, "a.pdf"); err == nil || !strings.Contains(err.Error(), "project path is required") {
		t.Fatalf("non-cloud: %v", err)
	}
	if _, err := app.MaterializeCloudAgentPreviewFile("cloud-agent:inst-1", "a.pdf"); err == nil || !strings.Contains(err.Error(), "project path is required") {
		t.Fatalf("short id: %v", err)
	}
}
