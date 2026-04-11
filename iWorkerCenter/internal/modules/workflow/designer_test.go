package workflow

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func setupDesignerDB(t *testing.T) (*db.Provider, *Service) {
	t.Helper()
	p := setupTestDB(t)
	wfRepo := NewRepo(p.Write, p.Read)
	collabRepo := collaboration.NewRepo(p.Write, p.Read)
	colRepo := colleagueRepo.New(p.Write, p.Read)
	svc := NewService(wfRepo, p, collabRepo, colRepo)
	return p, svc
}

func mockDesignerLLM(name string, steps []struct{ Name, Type, Role, Reject string }) LLMFunc {
	return func(systemPrompt, userPrompt string) (string, error) {
		out := map[string]any{
			"name":        name,
			"description": "AI 自动设计的流程",
		}
		var stepsJSON []map[string]string
		for _, s := range steps {
			stepsJSON = append(stepsJSON, map[string]string{
				"step_name":          s.Name,
				"step_type":          s.Type,
				"assignee_role_code": s.Role,
				"reject_rule":        s.Reject,
			})
		}
		out["steps"] = stepsJSON
		data, _ := json.Marshal(out)
		return string(data), nil
	}
}

func TestDesign_CreatesDefinition(t *testing.T) {
	_, svc := setupDesignerDB(t)
	designer := NewDesigner(svc, mockDesignerLLM("质量问题闭环", []struct{ Name, Type, Role, Reject string }{
		{"问题分析", "processing", "quality", "end_process"},
		{"整改执行", "processing", "production", "end_process"},
		{"效果审核", "review", "quality", "return_initiator"},
		{"归档通知", "archive", "office", "end_process"},
	}))

	result, err := designer.Design(testTenantID, DesignRequest{Description: "质量问题发现后需要分析、整改、审核、归档"})
	if err != nil {
		t.Fatalf("design: %v", err)
	}
	if result.Definition.Name != "质量问题闭环" {
		t.Errorf("expected name '质量问题闭环', got %s", result.Definition.Name)
	}
	if len(result.Steps) != 4 {
		t.Errorf("expected 4 steps, got %d", len(result.Steps))
	}
	if result.Definition.Status != DefStatusDraft {
		t.Errorf("expected draft, got %s", result.Definition.Status)
	}
}

func TestDesign_AutoPublish(t *testing.T) {
	_, svc := setupDesignerDB(t)
	designer := NewDesigner(svc, mockDesignerLLM("日报流转", []struct{ Name, Type, Role, Reject string }{
		{"撰写日报", "processing", "production", "end_process"},
		{"汇总审核", "review", "office", "end_process"},
	}))

	result, err := designer.Design(testTenantID, DesignRequest{
		Description: "每天生产同事写日报，办公同事汇总审核",
		AutoPublish: true,
	})
	if err != nil {
		t.Fatalf("design: %v", err)
	}
	if !result.Published {
		t.Error("expected auto-published")
	}
	if result.Definition.Status != DefStatusPublished {
		t.Errorf("expected published, got %s", result.Definition.Status)
	}
}

func TestDesign_EmptyDescription(t *testing.T) {
	_, svc := setupDesignerDB(t)
	designer := NewDesigner(svc, nil)

	_, err := designer.Design(testTenantID, DesignRequest{Description: ""})
	if err == nil {
		t.Error("expected error for empty description")
	}
}

func TestDesign_LLMError(t *testing.T) {
	_, svc := setupDesignerDB(t)
	designer := NewDesigner(svc, func(s, u string) (string, error) {
		return "", fmt.Errorf("LLM unavailable")
	})

	_, err := designer.Design(testTenantID, DesignRequest{Description: "test"})
	if err == nil {
		t.Error("expected error from LLM failure")
	}
}

func TestDesign_NoLLMFunction(t *testing.T) {
	_, svc := setupDesignerDB(t)
	designer := NewDesigner(svc, nil)

	_, err := designer.Design(testTenantID, DesignRequest{Description: "test"})
	if err == nil {
		t.Error("expected error when no LLM function")
	}
}
