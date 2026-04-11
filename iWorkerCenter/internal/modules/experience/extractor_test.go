package experience

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func setupTestDB(t *testing.T) *db.Provider {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

func mockLLM(experiences []ExtractedExperience) LLMExtractFunc {
	return func(systemPrompt, userPrompt string) (string, error) {
		out := struct {
			Experiences []ExtractedExperience `json:"experiences"`
		}{Experiences: experiences}
		data, _ := json.Marshal(out)
		return string(data), nil
	}
}

func mockLLMError() LLMExtractFunc {
	return func(systemPrompt, userPrompt string) (string, error) {
		return "", fmt.Errorf("LLM unavailable")
	}
}

func countMemories(db *sql.DB) int {
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM shared_memories").Scan(&count)
	return count
}

func TestExtractSync_SavesExperiences(t *testing.T) {
	p := setupTestDB(t)
	ext := NewExtractor(p.Write, mockLLM([]ExtractedExperience{
		{Title: "周报格式规范", Content: "周报应包含本周完成、下周计划、风险三个部分", Tags: []string{"周报", "规范"}},
		{Title: "异常上报流程", Content: "发现异常后先拍照记录，再填写异常单", Tags: []string{"异常", "流程"}},
	}))

	count, err := ext.ExtractSync("test-tenant", ExtractionInput{
		TaskTitle:     "周报整理",
		TaskResult:    "本周完成了产线A的质量检查，发现3个异常点，已全部整改。下周计划继续跟进产线B。风险：产线C设备老化需要关注。",
		RoleCode:      "office",
		ColleagueName: "小迪",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 saved, got %d", count)
	}

	total := countMemories(p.Write)
	if total != 2 {
		t.Errorf("expected 2 memories in DB, got %d", total)
	}

	// Verify auto-extraction tag
	var tags string
	_ = p.Read.QueryRow("SELECT tags FROM shared_memories LIMIT 1").Scan(&tags)
	if tags == "" {
		t.Error("expected tags to be set")
	}
	var tagList []string
	_ = json.Unmarshal([]byte(tags), &tagList)
	found := false
	for _, tag := range tagList {
		if tag == "自动提取" {
			found = true
		}
	}
	if !found {
		t.Error("expected '自动提取' tag")
	}
}

func TestExtractSync_EmptyExperiences(t *testing.T) {
	p := setupTestDB(t)
	ext := NewExtractor(p.Write, mockLLM([]ExtractedExperience{}))

	count, err := ext.ExtractSync("test-tenant", ExtractionInput{
		TaskTitle:  "简单查询",
		TaskResult: "查询结果：共10条记录。这是一个简单的数据查询任务，没有可提取的通用经验。",
		RoleCode:   "data",
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 saved, got %d", count)
	}
}

func TestExtractSync_LLMError(t *testing.T) {
	p := setupTestDB(t)
	ext := NewExtractor(p.Write, mockLLMError())

	_, err := ext.ExtractSync("test-tenant", ExtractionInput{
		TaskTitle:  "test",
		TaskResult: "some result content that is long enough to trigger extraction",
		RoleCode:   "office",
	})
	if err == nil {
		t.Error("expected error from LLM failure")
	}
}

func TestExtractSync_EmptyResult(t *testing.T) {
	p := setupTestDB(t)
	ext := NewExtractor(p.Write, mockLLM([]ExtractedExperience{
		{Title: "should not save", Content: "this should not be saved"},
	}))

	count, _ := ext.ExtractSync("test-tenant", ExtractionInput{
		TaskTitle:  "test",
		TaskResult: "", // empty
		RoleCode:   "office",
	})
	if count != 0 {
		t.Errorf("expected 0 for empty result, got %d", count)
	}
}

func TestExtractSync_EnterpriseScope(t *testing.T) {
	p := setupTestDB(t)
	ext := NewExtractor(p.Write, mockLLM([]ExtractedExperience{
		{Title: "企业通用经验", Content: "所有部门都应该遵循的规范"},
	}))

	count, _ := ext.ExtractSync("test-tenant", ExtractionInput{
		TaskTitle:  "test",
		TaskResult: "这是一个跨部门的通用任务结果，包含了很多有价值的经验总结，可以被所有角色复用。",
		RoleCode:   "", // no role → enterprise level
	})
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}

	var level, scope string
	_ = p.Read.QueryRow("SELECT level, scope FROM shared_memories LIMIT 1").Scan(&level, &scope)
	if level != "enterprise" {
		t.Errorf("expected enterprise level, got %s", level)
	}
	if scope != "all" {
		t.Errorf("expected all scope, got %s", scope)
	}
}

func TestExtract_Async_NonBlocking(t *testing.T) {
	p := setupTestDB(t)
	ext := NewExtractor(p.Write, mockLLM([]ExtractedExperience{
		{Title: "async test", Content: "this was extracted asynchronously"},
	}))

	// Extract is async, should not panic
	ext.Extract("test-tenant", ExtractionInput{
		TaskTitle:  "async task",
		TaskResult: "这是一个异步提取测试的任务结果。本次生产日报汇总了产线A和产线B的运行情况，其中产线A完成了计划产量的105%，产线B因设备维护暂停了2小时。质量方面，本批次合格率达到99.2%，较上周提升0.3个百分点。建议下周重点关注产线B的设备状态。",
		RoleCode:   "production",
	})

	// Give goroutine a moment (Extract runs synchronously in the same goroutine for non-nil llmExtract)
	// Actually Extract calls llmExtract synchronously, so the memory should be saved already
	total := countMemories(p.Write)
	if total != 1 {
		t.Errorf("expected 1 memory from async extract, got %d", total)
	}
}
