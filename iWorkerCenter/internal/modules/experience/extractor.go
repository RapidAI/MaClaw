package experience

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

// ExtractionInput holds the context for experience extraction.
type ExtractionInput struct {
	TaskTitle     string `json:"task_title"`
	TaskResult    string `json:"task_result"`
	RoleCode      string `json:"role_code"`
	ColleagueName string `json:"colleague_name"`
	WorkflowName  string `json:"workflow_name,omitempty"`
}

// ExtractedExperience is the structured output from LLM extraction.
type ExtractedExperience struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Level   string   `json:"level"` // role or team
	Scope   string   `json:"scope"` // role code or "all"
}

// LLMExtractFunc is the legacy function signature for calling LLM to extract experience.
// Callers inject the actual LLM call implementation.
type LLMExtractFunc func(systemPrompt, userPrompt string) (string, error)

// TenantLLMExtractFunc is the tenant-aware extractor used by iWorkerCenter runtime.
type TenantLLMExtractFunc func(tenantID, systemPrompt, userPrompt string) (string, error)

// Extractor handles automatic experience extraction and persistence.
type Extractor struct {
	write            *sql.DB
	llmExtract       LLMExtractFunc
	tenantLLMExtract TenantLLMExtractFunc
}

// NewExtractor creates an Extractor.
func NewExtractor(write *sql.DB, llmFn LLMExtractFunc) *Extractor {
	return &Extractor{write: write, llmExtract: llmFn}
}

// NewTenantExtractor creates an Extractor that routes LLM calls per tenant.
func NewTenantExtractor(write *sql.DB, llmFn TenantLLMExtractFunc) *Extractor {
	return &Extractor{write: write, tenantLLMExtract: llmFn}
}

const extractionSystemPrompt = `你是企业知识管理专家。你的任务是从已完成的工作结果中提取可复用的经验。

规则：
1. 只提取有通用价值的经验，不要提取一次性的具体数据
2. 经验标题要简洁明确（10-20字）
3. 经验内容要具体可操作（50-200字）
4. 标签用于分类检索（2-5个）
5. 如果结果中没有可提取的经验，返回空 JSON: {"experiences": []}

输出格式（严格 JSON）：
{"experiences": [{"title": "经验标题", "content": "经验内容", "tags": ["标签1", "标签2"]}]}`

// Extract analyzes a completed task and creates shared memories from extracted experiences.
// This is designed to be called asynchronously (non-blocking).
func (e *Extractor) Extract(tenantID string, input ExtractionInput) {
	if !e.hasLLM() {
		return
	}
	if strings.TrimSpace(input.TaskResult) == "" {
		return
	}
	// Skip very short results (likely not worth extracting)
	if len([]rune(input.TaskResult)) < 100 {
		return
	}

	userPrompt := fmt.Sprintf("任务标题：%s\n角色：%s\n同事：%s\n",
		input.TaskTitle, input.RoleCode, input.ColleagueName)
	if input.WorkflowName != "" {
		userPrompt += fmt.Sprintf("所属流程：%s\n", input.WorkflowName)
	}
	// Truncate result to avoid token overflow
	result := input.TaskResult
	if len([]rune(result)) > 2000 {
		result = string([]rune(result)[:2000]) + "..."
	}
	userPrompt += fmt.Sprintf("\n任务结果：\n%s", result)

	llmOutput, err := e.extractWithLLM(tenantID, extractionSystemPrompt, userPrompt)
	if err != nil {
		log.Printf("[experience] LLM extraction failed: %v", err)
		return
	}

	var parsed struct {
		Experiences []ExtractedExperience `json:"experiences"`
	}
	if err := json.Unmarshal([]byte(llmOutput), &parsed); err != nil {
		// Try to find JSON in the output
		if idx := strings.Index(llmOutput, "{"); idx >= 0 {
			if err2 := json.Unmarshal([]byte(llmOutput[idx:]), &parsed); err2 != nil {
				log.Printf("[experience] failed to parse LLM output: %v", err)
				return
			}
		} else {
			log.Printf("[experience] no JSON in LLM output")
			return
		}
	}

	if len(parsed.Experiences) == 0 {
		return
	}

	now := time.Now().Format(time.RFC3339)
	for _, exp := range parsed.Experiences {
		title := strings.TrimSpace(exp.Title)
		content := strings.TrimSpace(exp.Content)
		if title == "" || content == "" {
			continue
		}

		level := "role"
		scope := input.RoleCode
		if scope == "" {
			level = "enterprise"
			scope = "all"
		}

		tags := exp.Tags
		if tags == nil {
			tags = []string{}
		}
		// Add auto-extraction tag
		tags = append(tags, "自动提取")
		tagsJSON, _ := json.Marshal(tags)

		id := idgen.New("mem")
		_, err := e.write.Exec(`INSERT INTO shared_memories (id, tenant_id, title, content, level, scope, tags, version, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, 'active', ?, ?)`,
			id, tenantID, title, content, level, scope, string(tagsJSON), now, now)
		if err != nil {
			log.Printf("[experience] failed to save memory %q: %v", title, err)
			continue
		}
		log.Printf("[experience] auto-extracted: %q (scope=%s)", title, scope)
	}
}

// ExtractSync is like Extract but returns the count of saved experiences (for testing).
func (e *Extractor) ExtractSync(tenantID string, input ExtractionInput) (int, error) {
	if !e.hasLLM() {
		return 0, fmt.Errorf("no LLM function configured")
	}
	if strings.TrimSpace(input.TaskResult) == "" {
		return 0, nil
	}

	userPrompt := fmt.Sprintf("任务标题：%s\n角色：%s\n同事：%s\n\n任务结果：\n%s",
		input.TaskTitle, input.RoleCode, input.ColleagueName, input.TaskResult)

	llmOutput, err := e.extractWithLLM(tenantID, extractionSystemPrompt, userPrompt)
	if err != nil {
		return 0, fmt.Errorf("LLM call failed: %w", err)
	}

	var parsed struct {
		Experiences []ExtractedExperience `json:"experiences"`
	}
	if err := json.Unmarshal([]byte(llmOutput), &parsed); err != nil {
		if idx := strings.Index(llmOutput, "{"); idx >= 0 {
			_ = json.Unmarshal([]byte(llmOutput[idx:]), &parsed)
		}
	}

	saved := 0
	now := time.Now().Format(time.RFC3339)
	for _, exp := range parsed.Experiences {
		title := strings.TrimSpace(exp.Title)
		content := strings.TrimSpace(exp.Content)
		if title == "" || content == "" {
			continue
		}
		level := "role"
		scope := input.RoleCode
		if scope == "" {
			level = "enterprise"
			scope = "all"
		}
		tags := exp.Tags
		if tags == nil {
			tags = []string{}
		}
		tags = append(tags, "自动提取")
		tagsJSON, _ := json.Marshal(tags)

		id := idgen.New("mem")
		if _, err := e.write.Exec(`INSERT INTO shared_memories (id, tenant_id, title, content, level, scope, tags, version, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1, 'active', ?, ?)`,
			id, tenantID, title, content, level, scope, string(tagsJSON), now, now); err != nil {
			continue
		}
		saved++
	}
	return saved, nil
}

func (e *Extractor) hasLLM() bool {
	return e != nil && (e.llmExtract != nil || e.tenantLLMExtract != nil)
}

func (e *Extractor) extractWithLLM(tenantID, systemPrompt, userPrompt string) (string, error) {
	if e.tenantLLMExtract != nil {
		return e.tenantLLMExtract(tenantID, systemPrompt, userPrompt)
	}
	return e.llmExtract(systemPrompt, userPrompt)
}
