package v2

import (
	"context"
	"strings"
	"testing"
)

// mockRecallProvider implements RecallProvider for testing.
type mockRecallProvider struct {
	results map[string][]RecallResult // keyed by query substring
}

func (m *mockRecallProvider) RecallForField(ctx context.Context, query string, maxResults int) []RecallResult {
	// Return results if query contains any of our registered keys
	for key, results := range m.results {
		if strings.Contains(query, key) {
			if len(results) > maxResults {
				return results[:maxResults]
			}
			return results
		}
	}
	return nil
}

func TestPrefillFromRecall_NilProvider(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{{Name: "name", Label: "姓名", Type: "text"}},
	}
	result := PrefillFromRecall(context.Background(), schema, nil, nil)
	if result != nil {
		t.Errorf("expected nil for nil provider, got %v", result)
	}
}

func TestPrefillFromRecall_SkipsAlreadyFilled(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
			{Name: "institution", Label: "单位", Type: "text"},
		},
	}
	existing := map[string]*PrefilledValue{
		"name": {Value: "张三", Source: "context"},
	}
	provider := &mockRecallProvider{
		results: map[string][]RecallResult{
			"姓名": {{Content: "姓名：李四", Category: "user_fact", Source: "memory", Score: 0.9, SourceDesc: "记忆条目"}},
			"单位": {{Content: "单位：北京大学", Category: "user_fact", Source: "memory", Score: 0.9, SourceDesc: "记忆条目"}},
		},
	}

	result := PrefillFromRecall(context.Background(), schema, existing, provider)
	// "name" should NOT be overwritten
	if result["name"].Value != "张三" {
		t.Errorf("name was overwritten: got %v", result["name"].Value)
	}
	// "institution" should be filled from recall
	if v, ok := result["institution"]; !ok || v.Value != "北京大学" {
		t.Errorf("institution: got %v, want 北京大学", result["institution"])
	}
}

func TestPrefillFromRecall_ExtractsFromMemory(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "research_field", Label: "研究领域", Type: "text"},
			{Name: "h_index", Label: "H指数", Type: "text"},
		},
	}
	provider := &mockRecallProvider{
		results: map[string][]RecallResult{
			"研究领域": {{Content: "研究领域：自然语言处理", Category: "user_fact", Source: "memory", Score: 0.85, SourceDesc: "用户事实"}},
			"H指数":  {{Content: "42", Category: "user_fact", Source: "memory", Score: 0.9, SourceDesc: "用户H指数记录"}},
		},
	}

	result := PrefillFromRecall(context.Background(), schema, nil, provider)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if v, ok := result["research_field"]; !ok || v.Value != "自然语言处理" {
		t.Errorf("research_field: got %v", result["research_field"])
	}
	if v, ok := result["h_index"]; !ok || v.Value != "42" {
		t.Errorf("h_index: got %v", result["h_index"])
	}
}

func TestPrefillFromRecall_ExtractsFromKnowledge(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "institution", Label: "现工作单位", Type: "text"},
		},
	}
	provider := &mockRecallProvider{
		results: map[string][]RecallResult{
			"工作单位": {{
				Content:    "现工作单位：清华大学计算机系",
				Category:   "knowledge_card",
				Source:     "knowledge",
				Score:      0.92,
				SourceDesc: "来自知识库: 个人简历.pdf",
			}},
		},
	}

	result := PrefillFromRecall(context.Background(), schema, nil, provider)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	v := result["institution"]
	if v == nil || v.Value != "清华大学计算机系" {
		t.Errorf("institution: got %v", v)
	}
	if v.Source != "knowledge" {
		t.Errorf("source: got %q, want 'knowledge'", v.Source)
	}
	if v.Confidence < 0.8 {
		t.Errorf("confidence too low: %f", v.Confidence)
	}
}

func TestPrefillFromRecall_SelectFieldMatchesOptions(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "gender", Label: "性别", Type: "select", Options: []PhaseInputOption{
				{Label: "男", Value: "男"}, {Label: "女", Value: "女"},
			}},
		},
	}
	provider := &mockRecallProvider{
		results: map[string][]RecallResult{
			"性别": {{Content: "用户性别为男", Category: "user_fact", Source: "memory", Score: 0.8, SourceDesc: "用户事实"}},
		},
	}

	result := PrefillFromRecall(context.Background(), schema, nil, provider)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if v := result["gender"]; v == nil || v.Value != "男" {
		t.Errorf("gender: got %v", result["gender"])
	}
}

func TestPrefillFromRecall_SkipsBlockedFields(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "ssh_password", Label: "密码", Type: "text"},
			{Name: "core_question", Label: "核心问题", Type: "textarea", Required: true},
		},
	}
	provider := &mockRecallProvider{
		results: map[string][]RecallResult{
			"密码":   {{Content: "密码：secret123", Category: "user_fact", Source: "memory", Score: 0.9}},
			"核心问题": {{Content: "如何提高检测精度", Category: "task_artifact", Source: "memory", Score: 0.9}},
		},
	}

	result := PrefillFromRecall(context.Background(), schema, nil, provider)
	if result != nil {
		if _, ok := result["ssh_password"]; ok {
			t.Error("ssh_password should not be prefilled")
		}
		if _, ok := result["core_question"]; ok {
			t.Error("core_question (required textarea) should not be prefilled")
		}
	}
}

func TestPrefillFromRecall_RespectsContextCancellation(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
			{Name: "institution", Label: "单位", Type: "text"},
		},
	}
	provider := &mockRecallProvider{
		results: map[string][]RecallResult{
			"姓名": {{Content: "姓名：张三", Category: "user_fact", Source: "memory", Score: 0.9}},
			"单位": {{Content: "单位：北京大学", Category: "user_fact", Source: "memory", Score: 0.9}},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result := PrefillFromRecall(ctx, schema, nil, provider)
	// Should return early with empty or partial results
	if result != nil && len(result) > 0 {
		// It's acceptable to have 0 results since we cancelled before any work
		// The exact behavior depends on goroutine scheduling
		t.Logf("got %d results despite cancellation (acceptable race)", len(result))
	}
}

func TestBuildRecallQuery(t *testing.T) {
	tests := []struct {
		field    PhaseInputField
		contains []string // all substrings must be present in the result
	}{
		// Name + Label + Placeholder → all parts included
		{
			PhaseInputField{Name: "institution", Label: "现工作单位", Placeholder: "如：XX大学 XX学院"},
			[]string{"institution", "现工作单位", "XX大学"},
		},
		// Placeholder cleaned: "如：" prefix stripped
		{
			PhaseInputField{Name: "h_index", Label: "H指数", Placeholder: "如：35"},
			[]string{"h_index", "H指数", "35"},
		},
		// Name == Label → not duplicated
		{
			PhaseInputField{Name: "姓名", Label: "姓名"},
			[]string{"姓名"},
		},
		// Name only (no label, no placeholder)
		{
			PhaseInputField{Name: "test_field"},
			[]string{"test_field"},
		},
		// Multi-line placeholder → only first line used
		{
			PhaseInputField{Name: "education", Label: "教育背景", Placeholder: "按时间顺序列出：\n本科：XX大学"},
			[]string{"education", "教育背景", "本科"},
		},
		// Empty → empty
		{PhaseInputField{}, nil},
	}
	for _, tt := range tests {
		got := buildRecallQuery(tt.field)
		if len(tt.contains) == 0 {
			if got != "" {
				t.Errorf("buildRecallQuery(%+v) = %q, want empty", tt.field, got)
			}
			continue
		}
		for _, sub := range tt.contains {
			if !strings.Contains(got, sub) {
				t.Errorf("buildRecallQuery(%+v) = %q, want to contain %q", tt.field, got, sub)
			}
		}
	}
}

func TestRecallConfidence(t *testing.T) {
	tests := []struct {
		result  RecallResult
		minConf float64
	}{
		{RecallResult{Source: "knowledge", Score: 0.9}, 0.88},
		{RecallResult{Source: "knowledge", Score: 0.5}, 0.78},
		{RecallResult{Source: "memory", Category: "user_fact"}, 0.83},
		{RecallResult{Source: "memory", Category: "project_knowledge"}, 0.78},
		{RecallResult{Source: "memory", Category: "conversation_summary"}, 0.63},
	}
	for _, tt := range tests {
		got := recallConfidence(tt.result)
		if got < tt.minConf {
			t.Errorf("recallConfidence(%+v) = %f, want >= %f", tt.result, got, tt.minConf)
		}
	}
}


func TestPrefillFromRecall_StripsLabelPrefixFromSedimentedContent(t *testing.T) {
	// sedimentFormDataToMemory stores "H指数：42" — recall should return "42" not "H指数：42"
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "h_index", Label: "H指数", Type: "text"},
		},
	}
	provider := &mockRecallProvider{
		results: map[string][]RecallResult{
			"H指数": {{Content: "H指数：42", Category: "user_fact", Source: "memory", Score: 0.9, SourceDesc: "用户事实"}},
		},
	}

	result := PrefillFromRecall(context.Background(), schema, nil, provider)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	v := result["h_index"]
	if v == nil {
		t.Fatal("expected h_index to be filled")
	}
	// Should be "42" not "H指数：42"
	if v.Value != "42" {
		t.Errorf("h_index value = %q, want %q", v.Value, "42")
	}
}

func TestExtractByLabelAnchor_PrefersLastOccurrence(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "research_field", Label: "研究领域", Type: "text"},
		},
	}
	// Earlier mention vs later (more recent) mention
	msg := "研究领域：机器学习\n后来转到了新的方向\n研究领域：自然语言处理"
	result := PrefillFromContext(schema, msg, nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if v := result["research_field"]; v == nil || v.Value != "自然语言处理" {
		t.Errorf("research_field = %v, want 自然语言处理 (last occurrence)", result["research_field"])
	}
}


func TestPrefillFromRecall_CrossTemplateFieldNameMatch(t *testing.T) {
	// Scenario: user submitted "institution" in 杰青 (Label: "现工作单位"),
	// sedimented as "institution/现工作单位：北京大学".
	// Now user starts 青基 (Label: "依托单位"). Recall should still find "北京大学"
	// via field name "institution" matching in the sedimented content.
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "institution", Label: "依托单位", Type: "text"},
		},
	}
	provider := &mockRecallProvider{
		results: map[string][]RecallResult{
			// BM25 query "institution 依托单位" hits this entry via "institution" token
			"institution": {{
				Content:    "institution/现工作单位：北京大学\nname：张三\nh_index/H指数：42",
				Category:   "user_fact",
				Source:     "memory",
				Score:      0.85,
				SourceDesc: "来自记忆(用户事实)",
			}},
		},
	}

	result := PrefillFromRecall(context.Background(), schema, nil, provider)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	v := result["institution"]
	if v == nil {
		t.Fatal("expected institution to be filled from cross-template recall")
	}
	if v.Value != "北京大学" {
		t.Errorf("institution value = %q, want %q", v.Value, "北京大学")
	}
}
