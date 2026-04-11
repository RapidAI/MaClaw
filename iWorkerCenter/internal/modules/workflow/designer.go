package workflow

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// LLMFunc is the function signature for calling LLM.
type LLMFunc func(systemPrompt, userPrompt string) (string, error)

// Designer uses LLM to auto-generate workflow definitions from natural language descriptions.
type Designer struct {
	svc    *Service
	llmFn  LLMFunc
}

// NewDesigner creates a Designer.
func NewDesigner(svc *Service, llmFn LLMFunc) *Designer {
	return &Designer{svc: svc, llmFn: llmFn}
}

const designerSystemPrompt = `你是企业流程设计专家。根据用户描述的业务场景，设计一个工作流模板。

可用角色代码：office（办公）、data（数据）、production（生产）、quality（质量）

规则：
1. 每个步骤必须指定一个角色代码（assignee_role_code）
2. 步骤按执行顺序排列
3. 流程名称简洁明确（5-15字）
4. 每个步骤名称简洁（3-10字）
5. step_type 可选：processing（处理）、review（审核）、notification（通知）、archive（归档）
6. reject_rule 可选：end_process（终止流程）、return_initiator（退回发起人）

输出格式（严格 JSON）：
{
  "name": "流程名称",
  "description": "流程描述",
  "steps": [
    {"step_name": "步骤名", "step_type": "processing", "assignee_role_code": "quality", "reject_rule": "end_process"}
  ]
}`

// DesignRequest holds the input for AI-driven workflow design.
type DesignRequest struct {
	Description string `json:"description"`
	AutoPublish bool   `json:"auto_publish"`
}

// DesignResult holds the output of AI-driven workflow design.
type DesignResult struct {
	Definition *Definition       `json:"definition"`
	Steps      []*StepDefinition `json:"steps"`
	Published  bool              `json:"published"`
}

// Design generates a workflow definition from a natural language description.
func (d *Designer) Design(tenantID string, req DesignRequest) (*DesignResult, error) {
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		return nil, fmt.Errorf("description is required")
	}
	if d.llmFn == nil {
		return nil, fmt.Errorf("LLM function not configured")
	}

	llmOutput, err := d.llmFn(designerSystemPrompt, desc)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	var parsed struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Steps       []struct {
			StepName         string `json:"step_name"`
			StepType         string `json:"step_type"`
			AssigneeRoleCode string `json:"assignee_role_code"`
			RejectRule       string `json:"reject_rule"`
		} `json:"steps"`
	}

	// Try to parse JSON from LLM output
	if err := json.Unmarshal([]byte(llmOutput), &parsed); err != nil {
		if idx := strings.Index(llmOutput, "{"); idx >= 0 {
			if err2 := json.Unmarshal([]byte(llmOutput[idx:]), &parsed); err2 != nil {
				return nil, fmt.Errorf("failed to parse LLM output: %w", err)
			}
		} else {
			return nil, fmt.Errorf("no JSON in LLM output")
		}
	}

	if parsed.Name == "" {
		return nil, fmt.Errorf("LLM did not generate a workflow name")
	}
	if len(parsed.Steps) == 0 {
		return nil, fmt.Errorf("LLM did not generate any steps")
	}

	// Convert to CreateDefinitionRequest
	createReq := CreateDefinitionRequest{
		Name:        parsed.Name,
		Description: parsed.Description,
		TriggerType: "manual",
	}
	for i, s := range parsed.Steps {
		createReq.Steps = append(createReq.Steps, CreateStepDefRequest{
			StepCode:         fmt.Sprintf("step_%d", i+1),
			StepName:         s.StepName,
			StepType:         defaultStr(s.StepType, "processing"),
			AssigneeMode:     "by_role",
			AssigneeRoleCode: s.AssigneeRoleCode,
			RejectRule:       defaultStr(s.RejectRule, "end_process"),
		})
	}

	def, err := d.svc.CreateDefinition(tenantID, createReq)
	if err != nil {
		return nil, fmt.Errorf("create definition: %w", err)
	}

	steps, _ := d.svc.ListStepDefinitions(tenantID, def.ID)

	result := &DesignResult{
		Definition: def,
		Steps:      steps,
	}

	if req.AutoPublish {
		if err := d.svc.PublishDefinition(tenantID, def.ID); err != nil {
			log.Printf("[workflow-designer] auto-publish failed: %v", err)
		} else {
			def.Status = DefStatusPublished
			result.Published = true
		}
	}

	return result, nil
}
