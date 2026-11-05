package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestDiscoverToolImageNeedDoesNotAdmitUnrelatedTools(t *testing.T) {
	registry := NewToolRegistry()
	for _, tool := range []RegisteredTool{
		{Name: "asr", Description: audioconv.ASRToolDescription(), Category: ToolCategoryBuiltin, Status: RegToolAvailable, Tags: []string{"asr", "语音", "转写"}},
		{Name: "manage_skill", Description: cskill.ManageSkillDescription(), Category: ToolCategoryBuiltin, Status: RegToolAvailable, Tags: []string{"skill"}},
		{Name: "generate_pdf", Description: "生成 PDF 文档。将正文渲染为 PDF。", Category: ToolCategoryBuiltin, Status: RegToolAvailable, Tags: []string{"pdf"}},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatalf("Register %s: %v", tool.Name, err)
		}
	}
	h := &IMMessageHandler{registry: registry}
	out := h.toolDiscoverTool(map[string]interface{}{"need": "生成图片、画图、image generation、文生图"})
	if strings.Contains(out, "asr") || strings.Contains(out, "manage_skill") || strings.Contains(out, "call it now") || strings.Contains(out, "search/install") {
		t.Fatalf("image generation must not be answered with unrelated tools:\n%s", out)
	}
	if !strings.Contains(out, "No catalog tool matches") {
		t.Fatalf("missing capability must be reported as a catalog miss, got %s", out)
	}
}

func TestDiscoverToolPublishesManageSkillCallContract(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "manage_skill", Description: cskill.ManageSkillDescription(), Category: ToolCategoryBuiltin, Status: RegToolAvailable,
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{registry: registry}
	out := h.toolDiscoverTool(map[string]interface{}{"need": "manage_skill"})
	if strings.Contains(out, "search/install") || strings.Contains(out, "upload") {
		t.Fatalf("discovery published actions the surface rejects:\n%s", out)
	}
	if !strings.Contains(out, "list、info、run") && !strings.Contains(out, "list、info 或 run") {
		t.Fatalf("discovery must publish the callable manage_skill contract, got %s", out)
	}
}
