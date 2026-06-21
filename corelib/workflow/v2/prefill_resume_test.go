package v2

import (
	"strings"
	"testing"
)

func TestExtractJSONWithFieldsKey_CleanJSON(t *testing.T) {
	input := `{"fields": {"name": "张三"}, "confidence": {"name": 0.95}}`
	got := extractJSONWithFieldsKey(input)
	if got != input {
		t.Errorf("expected full input, got %q", got)
	}
}

func TestExtractJSONWithFieldsKey_PreambleText(t *testing.T) {
	input := `好的，以下是提取结果：
{"fields": {"name": "张三", "h_index": "42"}, "confidence": {"name": 0.9}}`
	got := extractJSONWithFieldsKey(input)
	if got == "" {
		t.Fatal("expected non-empty result")
	}
	if got[0] != '{' {
		t.Errorf("expected to start with {, got %q", got[:10])
	}
	if !contains(got, `"name": "张三"`) {
		t.Errorf("expected name field in result, got %q", got)
	}
}

func TestExtractJSONWithFieldsKey_BracesInStringValues(t *testing.T) {
	// Value contains braces inside a JSON string — should not break depth counting
	input := `{"fields": {"name": "张{三}院士", "title": "教授"}, "confidence": {}}`
	got := extractJSONWithFieldsKey(input)
	if got != input {
		t.Errorf("expected full input, got %q", got)
	}
}

func TestExtractJSONWithFieldsKey_EscapedQuotesInString(t *testing.T) {
	// Escaped quotes inside string values
	input := `{"fields": {"name": "张\"三\"", "title": "教授"}}`
	got := extractJSONWithFieldsKey(input)
	if got != input {
		t.Errorf("expected full input, got %q", got)
	}
}

func TestExtractJSONWithFieldsKey_NoFieldsKey(t *testing.T) {
	input := `{"data": {"name": "张三"}}`
	got := extractJSONWithFieldsKey(input)
	if got != "" {
		t.Errorf("expected empty for no 'fields' key, got %q", got)
	}
}

func TestExtractJSONWithFieldsKey_NestedBraces(t *testing.T) {
	input := `前言 {"fields": {"education": "本科{2010-2014}硕士{2014-2017}"}, "confidence": {"education": 0.8}} 后文`
	got := extractJSONWithFieldsKey(input)
	if got == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(got, `"education"`) {
		t.Errorf("expected education field, got %q", got)
	}
}

func TestBuildResumeParseSystemPrompt_SkipsNoPrefillFields(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
			{Name: "project_title", Label: "项目名称", Type: "text"},  // noPrefillFieldNames
			{Name: "core_question", Label: "科学问题", Type: "textarea"}, // noPrefillFieldNames
			{Name: "institution", Label: "单位", Type: "text"},
		},
	}
	prompt := buildResumeParseSystemPrompt(collectAllSchemaFields(schema))

	if !contains(prompt, "name") {
		t.Error("prompt should include 'name' field")
	}
	if !contains(prompt, "institution") {
		t.Error("prompt should include 'institution' field")
	}
	if contains(prompt, "project_title") {
		t.Error("prompt should NOT include 'project_title' (noPrefillFieldNames)")
	}
	if contains(prompt, "core_question") {
		t.Error("prompt should NOT include 'core_question' (noPrefillFieldNames)")
	}
}

func TestResumeParseResultToPrefilled_FiltersUnknownFields(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
			{Name: "institution", Label: "单位", Type: "text"},
		},
	}
	result := &ResumeParseResult{
		Fields: map[string]interface{}{
			"name":          "张三",
			"institution":   "北京大学",
			"hallucinated":  "不存在的字段",
		},
		Confidence:   map[string]float64{"name": 0.95},
		SourceQuotes: map[string]string{},
	}

	prefilled := ResumeParseResultToPrefilled(result, schema)

	if _, ok := prefilled["name"]; !ok {
		t.Error("expected 'name' in prefilled")
	}
	if _, ok := prefilled["institution"]; !ok {
		t.Error("expected 'institution' in prefilled")
	}
	if _, ok := prefilled["hallucinated"]; ok {
		t.Error("'hallucinated' should be filtered out (not in schema)")
	}
}

func TestResumeParseResultToPrefilled_SkipsEmptyValues(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
			{Name: "h_index", Label: "H指数", Type: "text"},
		},
	}
	result := &ResumeParseResult{
		Fields:       map[string]interface{}{"name": "张三", "h_index": "  "},
		Confidence:   map[string]float64{},
		SourceQuotes: map[string]string{},
	}

	prefilled := ResumeParseResultToPrefilled(result, schema)

	if _, ok := prefilled["name"]; !ok {
		t.Error("expected 'name' in prefilled")
	}
	if _, ok := prefilled["h_index"]; ok {
		t.Error("'h_index' should be skipped (whitespace-only value)")
	}
}

func TestShouldRecallPrefill_ReusableBypassesLegacyGate(t *testing.T) {
	// A field that is Reusable=true should always be recalled,
	// even if it's not in the old factualTextareaFields whitelist.
	field := PhaseInputField{Name: "prior_nsfc", Type: "textarea", Reusable: true}
	if !ShouldRecallPrefill(field, true) {
		t.Error("Reusable=true field should pass ShouldRecallPrefill")
	}
}

func TestShouldRecallPrefill_NonReusableBlockedWhenSchemaAnnotated(t *testing.T) {
	field := PhaseInputField{Name: "project_title", Type: "text", Reusable: false}
	if ShouldRecallPrefill(field, true) {
		t.Error("Reusable=false field should be blocked when schema has Reusable fields")
	}
}

func TestShouldRecallPrefill_LegacySchemaAllowsAll(t *testing.T) {
	// Legacy schema (no Reusable fields) — all non-blacklisted fields allowed
	field := PhaseInputField{Name: "topic", Type: "text", Reusable: false}
	if !ShouldRecallPrefill(field, false) {
		t.Error("legacy schema should allow all non-blacklisted fields")
	}
}

func TestShouldRecallPrefill_BlacklistAlwaysBlocks(t *testing.T) {
	field := PhaseInputField{Name: "ssh_password", Type: "text", Reusable: true}
	if ShouldRecallPrefill(field, true) {
		t.Error("blacklisted field should always be blocked regardless of Reusable")
	}
}

func TestSchemaHasReusableFields_DetectsAnnotated(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "a", Reusable: false},
			{Name: "b", Reusable: true},
		},
	}
	if !SchemaHasReusableFields(schema) {
		t.Error("expected true when at least one field is Reusable")
	}
}

func TestSchemaHasReusableFields_LegacySchema(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "a", Reusable: false},
			{Name: "b", Reusable: false},
		},
	}
	if SchemaHasReusableFields(schema) {
		t.Error("expected false when no field is Reusable")
	}
}

func TestAcademicPhaseIDIndex_ContainsAllExpectedIDs(t *testing.T) {
	expectedIDs := []string{
		"cj_profile", "cj_foundation", "cj_plan", "cj_phase4", "cj_assembly",
		"dy_profile", "dy_foundation", "dy_plan", "dy_phase4", "dy_assembly",
		"ey_profile", "ey_foundation", "ey_plan", "ey_phase4", "ey_assembly",
		"yf_profile", "yf_foundation", "yf_plan", "yf_phase4", "yf_assembly",
		"gp_profile", "gp_foundation", "gp_plan", "gp_phase4", "gp_assembly",
		"kp_profile", "kp_foundation", "kp_plan", "kp_phase4", "kp_assembly",
	}
	for _, id := range expectedIDs {
		if _, ok := academicPhaseIDIndex[id]; !ok {
			t.Errorf("expected %q in academicPhaseIDIndex", id)
		}
	}
}

func TestAcademicPhaseIDIndex_DoesNotContainOldIDs(t *testing.T) {
	oldIDs := []string{
		"cj_personal_profile", "dy_eligibility", "ey_eligibility",
		"yf_rationale", "gp_rationale", "kp_strategic_rationale",
	}
	for _, id := range oldIDs {
		if _, ok := academicPhaseIDIndex[id]; ok {
			t.Errorf("old phase ID %q should NOT be in academicPhaseIDIndex (backward compat via switch fallback)", id)
		}
	}
}

func TestAcademicPhaseIDIndex_NoPrefixCollisions(t *testing.T) {
	// Verify no review template phase IDs accidentally match
	reviewPhaseIDs := []string{
		"cj_completeness_check", "cj_achievement_evaluation",
		"dy_review_completeness", "ey_review_completeness",
	}
	for _, id := range reviewPhaseIDs {
		if _, ok := academicPhaseIDIndex[id]; ok {
			t.Errorf("review phase ID %q should NOT match academic application index", id)
		}
	}
}

// helper
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// --- Tests for Variant-based schema (academic templates) ---

func TestCollectAllSchemaFields_TopLevelOnly(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
			{Name: "institution", Label: "单位", Type: "text"},
		},
	}
	fields := collectAllSchemaFields(schema)
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
}

func TestCollectAllSchemaFields_VariantOnly(t *testing.T) {
	// Simulates academic template structure: top-level Fields is empty,
	// actual fields are inside Variants.
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{}, // empty!
		Variants: []PhaseInputVariant{
			{ID: "resume_mode", Fields: []PhaseInputField{
				{Name: "resume_file", Label: "简历文件", Type: "file"},
			}},
			{ID: "manual_mode", Fields: []PhaseInputField{
				{Name: "name", Label: "姓名", Type: "text"},
				{Name: "institution", Label: "依托单位", Type: "text"},
				{Name: "gender", Label: "性别", Type: "select", Options: []PhaseInputOption{
					{Label: "男", Value: "男"}, {Label: "女", Value: "女"},
				}},
			}},
		},
	}
	fields := collectAllSchemaFields(schema)
	// Should include: resume_file + name + institution + gender = 4
	if len(fields) != 4 {
		t.Fatalf("expected 4 fields from variants, got %d", len(fields))
	}
	// Verify names
	names := make(map[string]bool)
	for _, f := range fields {
		names[f.Name] = true
	}
	for _, expected := range []string{"resume_file", "name", "institution", "gender"} {
		if !names[expected] {
			t.Errorf("expected field %q in result", expected)
		}
	}
}

func TestCollectAllSchemaFields_DeduplicatesByName(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名 (top-level)", Type: "text"},
		},
		Variants: []PhaseInputVariant{
			{ID: "v1", Fields: []PhaseInputField{
				{Name: "name", Label: "姓名 (variant)", Type: "text"}, // duplicate
				{Name: "age", Label: "年龄", Type: "text"},
			}},
		},
	}
	fields := collectAllSchemaFields(schema)
	if len(fields) != 2 { // name (first occurrence) + age
		t.Fatalf("expected 2 deduped fields, got %d", len(fields))
	}
	// First occurrence wins — top-level label
	if fields[0].Label != "姓名 (top-level)" {
		t.Errorf("expected top-level label to win, got %q", fields[0].Label)
	}
}

func TestCollectAllSchemaFields_NilAndEmpty(t *testing.T) {
	if got := collectAllSchemaFields(nil); got != nil {
		t.Errorf("nil schema should return nil, got %v", got)
	}
	empty := &PhaseInputSchema{}
	if got := collectAllSchemaFields(empty); got != nil {
		t.Errorf("empty schema should return nil, got %v", got)
	}
}

func TestBuildResumeParseSystemPrompt_VariantSchema_SkipsFileFields(t *testing.T) {
	// Simulates the exact academic template structure that caused the original bug
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{}, // empty top-level
		Variants: []PhaseInputVariant{
			{ID: "resume_mode", Fields: []PhaseInputField{
				{Name: "resume_file", Label: "简历文件", Type: "file"},
			}},
			{ID: "manual_mode", Fields: []PhaseInputField{
				{Name: "name", Label: "姓名", Type: "text"},
				{Name: "h_index", Label: "H指数", Type: "text"},
			}},
		},
	}
	allFields := collectAllSchemaFields(schema)
	prompt := buildResumeParseSystemPrompt(allFields)

	if !contains(prompt, "name") {
		t.Error("prompt should include 'name' field from manual_mode variant")
	}
	if !contains(prompt, "h_index") {
		t.Error("prompt should include 'h_index' field from manual_mode variant")
	}
	if contains(prompt, "resume_file") {
		t.Error("prompt should NOT include 'resume_file' (file type field)")
	}
}

func TestSchemaHasReusableFields_DetectsInVariants(t *testing.T) {
	// Reusable fields are inside variants, not at top level
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{}, // empty top-level
		Variants: []PhaseInputVariant{
			{ID: "manual_mode", Fields: []PhaseInputField{
				{Name: "name", Label: "姓名", Type: "text", Reusable: true},
				{Name: "project_title", Label: "项目名称", Type: "text", Reusable: false},
			}},
		},
	}
	if !SchemaHasReusableFields(schema) {
		t.Error("should detect Reusable=true in variant fields")
	}
}

func TestResumeParseResultToPrefilled_AcceptsVariantFields(t *testing.T) {
	// Schema has fields only in variants — prefill should still work
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{}, // empty
		Variants: []PhaseInputVariant{
			{ID: "manual_mode", Fields: []PhaseInputField{
				{Name: "name", Label: "姓名", Type: "text"},
				{Name: "institution", Label: "单位", Type: "text"},
			}},
		},
	}
	result := &ResumeParseResult{
		Fields:       map[string]interface{}{"name": "张三", "institution": "北大"},
		Confidence:   map[string]float64{},
		SourceQuotes: map[string]string{},
	}
	prefilled := ResumeParseResultToPrefilled(result, schema)
	if prefilled == nil {
		t.Fatal("expected non-nil prefilled map")
	}
	if _, ok := prefilled["name"]; !ok {
		t.Error("expected 'name' in prefilled (from variant)")
	}
	if _, ok := prefilled["institution"]; !ok {
		t.Error("expected 'institution' in prefilled (from variant)")
	}
}

func TestPhaseInputSchema_AllFields_Method(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{{Name: "a"}},
		Variants: []PhaseInputVariant{
			{ID: "v1", Fields: []PhaseInputField{{Name: "b"}, {Name: "c"}}},
		},
	}
	all := schema.AllFields()
	if len(all) != 3 {
		t.Fatalf("expected 3 fields from AllFields(), got %d", len(all))
	}
}


func TestNormalizeDateToISO(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Chinese format with day
		{"1979年3月21日", "1979-03-21"},
		{"1980年12月5日", "1980-12-05"},
		{"1985年01月15日", "1985-01-15"},
		// Chinese format without day (month only)
		{"1980年5月", "1980-05"},
		{"1979年12月", "1979-12"},
		// Already ISO
		{"1979-03-21", "1979-03-21"},
		{"1980-05", "1980-05"},
		// ISO with trailing time (should truncate)
		{"1979-03-21T00:00:00Z", "1979-03-21"},
		// Empty/invalid
		{"", ""},
		{"未知", ""},
		{"abc", ""},
		// Edge cases
		{"1979年0月", ""},  // invalid month
		{"1979年13月", ""}, // invalid month
	}
	for _, tt := range tests {
		got := normalizeDateToISO(tt.input)
		if got != tt.want {
			t.Errorf("normalizeDateToISO(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
